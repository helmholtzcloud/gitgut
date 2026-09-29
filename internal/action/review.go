package action

import (
	"fmt"
	"time"

	"slices"

	"google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/agent/llmagent"
	"google.golang.org/adk/v2/workflow"
)

type ReviewActionConfig struct {
	Model  string `mapstructure:"model"`
	Prompt string `mapstructure:"prompt"`
}

type Review struct {
	Name            string
	Config          *ReviewActionConfig
	WorkflowContext *WorkflowContext
}

func (r *Review) GetSubgraph() (*Subgraph, error) {
	// Conduct a multi review

	guidelines := "## Project Guidelines\n\n"
	changedFiles := "## Current File Contents\n\n"

	for filePath, fileContents := range r.WorkflowContext.RepoTree {
		if slices.Contains(r.WorkflowContext.Config.Review.GuidelinePaths, filePath) {
			guidelines += fmt.Sprintf("### %s\n\n```\n%s\n```\n\n", filePath, fileContents)
		} else if slices.Contains(r.WorkflowContext.ChangedFiles, filePath) {
			changedFiles += fmt.Sprintf("### %s\n\n```\n%s\n```\n\n", filePath, fileContents)
		}
	}

	reviewContext := fmt.Sprintf("# Context\n\n%s\n\n%s\n\n## Proposed Change (Diff)\n\n```\n%s```\n\n\n", guidelines, changedFiles, r.WorkflowContext.Diff)

	instruction := fmt.Sprintf("%s\n\n # Your Task\n\n %s\n IMPORTANT: Output PLAIN JSON only, no additional text, no Markdown wrappers, just plain parseable JSON in this format: {\"findings\": [ { \"summary\": \"Short headline summary of the finding.\", \"description\": \"Detailed description of the finding (JSON-escaped Markdown).\" } ] }\n", reviewContext, r.Config.Prompt)

	subgraph := make(Subgraph, 0)

	model, err := r.WorkflowContext.Config.GetModel(r.Config.Model)
	if err != nil {
		return nil, err
	}

	// Create review node
	agent, err := llmagent.New(llmagent.Config{
		Name:                r.Name,
		Model:               model,
		Description:         "Conduct a code review",
		GlobalInstruction:   r.WorkflowContext.Config.Review.SystemPrompt,
		InstructionProvider: func(_ agent.ReadonlyContext) (string, error) { return instruction, nil },
	})
	if err != nil {
		return nil, err
	}

	node, err := workflow.NewAgentNodeTyped[string, ReviewResult](
		agent, workflow.NodeConfig{
			Timeout:     time.Minute * 10,
			RetryConfig: workflow.DefaultRetryConfig(),
		},
	)
	if err != nil {
		return nil, err
	}

	subgraph = append(subgraph, SubgraphItem{
		Name:      r.Name,
		DependsOn: nil,
		Node:      node,
	})

	return &subgraph, nil
}
