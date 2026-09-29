package action

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"slices"

	"google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/agent/llmagent"
	"google.golang.org/adk/v2/session"
	"google.golang.org/adk/v2/workflow"
)

type MultiReviewActionConfig struct {
	ReviewModel      string `mapstructure:"review_model"`
	AggregationModel string `mapstructure:"aggregation_model"`
	Reviews          uint8  `mapstructure:"reviews"`
	Quorum           uint8  `mapstructure:"quorum"`
	Prompt           string `mapstructure:"prompt"`
}

type MultiReview struct {
	Name            string
	Config          *MultiReviewActionConfig
	WorkflowContext *WorkflowContext
}

func (m *MultiReview) GetSubgraph() (*Subgraph, error) {
	// Conduct a multi review

	guidelines := "## Project Guidelines\n\n"
	changedFiles := "## Current File Contents\n\n"

	for filePath, fileContents := range m.WorkflowContext.RepoTree {
		if slices.Contains(m.WorkflowContext.Config.Review.GuidelinePaths, filePath) {
			guidelines += fmt.Sprintf("### %s\n\n```\n%s\n```\n\n", filePath, fileContents)
		} else if slices.Contains(m.WorkflowContext.ChangedFiles, filePath) {
			changedFiles += fmt.Sprintf("### %s\n\n```\n%s\n```\n\n", filePath, fileContents)
		}
	}

	reviewContext := fmt.Sprintf("# Context\n\n%s\n\n%s\n\n## Proposed Change (Diff)\n\n```\n%s```\n\n\n", guidelines, changedFiles, m.WorkflowContext.Diff)

	instruction := fmt.Sprintf("%s\n\n # Your Task\n\n %s\n IMPORTANT: Output PLAIN JSON only, no additional text, no Markdown wrappers, just plain parseable JSON in this format: {\"findings\": [ { \"summary\": \"Short headline summary of the finding.\", \"description\": \"Detailed description of the finding.\" } ] }\n", reviewContext, m.Config.Prompt)

	subgraph := make(Subgraph, 0)

	// Create review node (one at a time)
	multiReviewNodeName := fmt.Sprintf("%s/multi_review", m.Name)
	multiReviewNode := workflow.NewDynamicNode(multiReviewNodeName,
		func(ctx agent.Context, _input string, _ func(*session.Event) error) (ReviewResult, error) {
			overallResult := ReviewResult{FindingsList: make([]Finding, 0)}

			slog.Debug("Starting multi-review", "workflow_action", m.Name)

			for i := 0; i < int(m.Config.Reviews); i++ {
				nodeName := fmt.Sprintf("gitgut/%s/review_%d", m.Name, i)

				slog.Debug("Starting review", "node", nodeName, "review", i+1, "of", m.Config.Reviews)

				result := ReviewResult{}
				model, err := m.WorkflowContext.Config.GetModel(m.Config.ReviewModel)
				if err != nil {
					return overallResult, err
				}

				reviewAgent, err := llmagent.New(llmagent.Config{
					Name:              nodeName,
					Model:             model,
					Description:       "Conduct a code review",
					GlobalInstruction: m.WorkflowContext.Config.Review.SystemPrompt,
				})
				if err != nil {
					return result, err
				}

				reviewNode, err := workflow.NewAgentNodeTyped[string, ReviewResult](
					reviewAgent, workflow.NodeConfig{
						RetryConfig: workflow.DefaultRetryConfig(),
					},
				)
				if err != nil {
					return result, err
				}

				findingsMap, err := workflow.RunNode[map[string]interface{}](ctx, reviewNode, instruction)
				if err != nil {
					return result, err
				}

				rawFindings, err := json.Marshal(findingsMap)
				if err != nil {
					return result, err
				}

				err = json.Unmarshal([]byte(rawFindings), &result)
				if err != nil {
					return result, err
				}

				slog.Debug("Finished review", "node", nodeName, "review", i+1, "of", m.Config.Reviews, "findings", len(result.FindingsList))

				overallResult.FindingsList = append(overallResult.FindingsList, result.FindingsList...)
			}

			slog.Debug("Finished multi-review", "workflow_action", m.Name, "findings", len(overallResult.FindingsList))

			return overallResult, nil
		},
		workflow.NodeConfig{
			RetryConfig: workflow.DefaultRetryConfig(),
			Timeout:     time.Minute * 15,
		},
	)

	subgraph = append(subgraph, SubgraphItem{
		Name:      multiReviewNodeName,
		DependsOn: nil,
		Node:      multiReviewNode,
	})

	aggregationModel, err := m.WorkflowContext.Config.GetModel(m.Config.AggregationModel)
	if err != nil {
		return nil, err
	}

	// Aggregate results
	aggregationAgent, err := llmagent.New(llmagent.Config{
		Name:              m.Name,
		Model:             aggregationModel,
		Description:       "Aggregate reviews according to quorum",
		GlobalInstruction: m.WorkflowContext.Config.Review.SystemPrompt,
		InstructionProvider: func(_ agent.ReadonlyContext) (string, error) {
			return fmt.Sprintf("Here's a list of code review findings yieled by multiple sub-agents. Return findings which were found AT LEAST %d TIMES. \nIMPORTANT: You must always output pure JSON: {\"findings\": [ { \"summary\": \"Short headline summary of the finding.\", \"description\": \"Detailed description of the finding.\" } ] }\n", m.Config.Quorum), nil
		},
	})
	if err != nil {
		return nil, err
	}

	aggregationNode, err := workflow.NewAgentNodeTyped[ReviewResult, ReviewResult](
		aggregationAgent, workflow.NodeConfig{
			Timeout:     time.Minute * 5,
			RetryConfig: workflow.DefaultRetryConfig(),
		},
	)
	if err != nil {
		return nil, err
	}

	subgraph = append(subgraph, SubgraphItem{
		Name:      m.Name,
		DependsOn: &[]string{multiReviewNodeName},
		Node:      aggregationNode,
	})

	return &subgraph, nil
}
