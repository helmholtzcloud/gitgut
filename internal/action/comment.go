package action

import (
	"fmt"
	"time"

	"google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/agent/llmagent"
	"google.golang.org/adk/v2/workflow"
)

const NOTES_FROM_VERIFICATION_PREFIX = "*Notes from verification:* "

const LLM_NOTE = "> [!note] This comment is AI generated.\n"

type GenerateCommentActionConfig struct {
	LLMNoteSuffix string `mapstructure:"llm_note_suffix"`
	SummaryModel  string `mapstructure:"summary_model"`
	SummaryPrompt string `mapstructure:"summary_prompt"`
}

type GenerateComment struct {
	Name            string
	Config          *GenerateCommentActionConfig
	WorkflowContext *WorkflowContext
}

func (c *GenerateComment) GetSubgraph() (*Subgraph, error) {
	subgraph := make(Subgraph, 0)

	joinInputNodeName := fmt.Sprintf("gitgut/%s/joinInputs", c.Name)
	joinInputNode := workflow.NewJoinNode(joinInputNodeName)

	subgraph = append(subgraph, SubgraphItem{
		Name:      joinInputNodeName,
		DependsOn: c.WorkflowContext.Config.Review.Workflow[c.Name].InputFrom,
		Node:      joinInputNode,
	})

	markdownNodeName := fmt.Sprintf("gitgut/%s/markdown", c.Name)
	subgraph = append(subgraph, SubgraphItem{
		Name:      markdownNodeName,
		DependsOn: &[]string{joinInputNodeName},
		Node: workflow.NewFunctionNode(markdownNodeName,
			func(_ agent.Context, findings map[string]VerifiedReviewResult) (string, error) {
				markdown := ""

				for _, results := range findings {
					for _, finding := range results.FindingsList {
						markdown += fmt.Sprintf("<details>\n<summary>📑 %s</summary>\n\n%s\n", finding.Summary, finding.Description)

						if finding.NotesFromVerification != "" {
							markdown += fmt.Sprintf("\n%s%s\n", NOTES_FROM_VERIFICATION_PREFIX, finding.NotesFromVerification)
						}

						markdown += "</details>\n\n"
					}
				}

				return markdown, nil
			},
			workflow.NodeConfig{},
		),
	})

	summaryNodeName := fmt.Sprintf("gitgut/%s/summary", c.Name)
	summaryModel, err := c.WorkflowContext.Config.GetModel(c.Config.SummaryModel)
	if err != nil {
		return nil, err
	}

	summaryAgent, err := llmagent.New(llmagent.Config{
		Name:              summaryNodeName,
		Model:             summaryModel,
		Description:       "Summarize Findings",
		GlobalInstruction: c.WorkflowContext.Config.Review.SystemPrompt,
		InstructionProvider: func(_ agent.ReadonlyContext) (string, error) {
			return fmt.Sprintf("Summarize the findings your (GitGut) code review has yielded in pretty markdown: %s", c.Config.SummaryPrompt), nil
		},
	})
	if err != nil {
		return nil, err
	}

	summaryNode, err := workflow.NewAgentNodeTyped[map[string]VerifiedReviewResult, string](
		summaryAgent, workflow.NodeConfig{
			Timeout:     time.Minute * 5,
			RetryConfig: workflow.DefaultRetryConfig(),
		},
	)
	if err != nil {
		return nil, err
	}

	subgraph = append(subgraph, SubgraphItem{
		Name:      summaryNodeName,
		DependsOn: &[]string{joinInputNodeName},
		Node:      summaryNode,
	})

	joinInnerNodeName := fmt.Sprintf("gitgut/%s/joinInner", c.Name)
	joinInnerNode := workflow.NewJoinNode(joinInnerNodeName)

	subgraph = append(subgraph, SubgraphItem{
		Name:      joinInnerNodeName,
		DependsOn: &[]string{summaryNodeName, markdownNodeName},
		Node:      joinInnerNode,
	})

	subgraph = append(subgraph, SubgraphItem{
		Name:      c.Name,
		DependsOn: &[]string{joinInnerNodeName},
		Node: workflow.NewFunctionNode(c.Name,
			func(_ agent.Context, inputs map[string]string) (string, error) {
				output := LLM_NOTE + c.Config.LLMNoteSuffix + "\n\n---\n\n" + inputs[summaryNodeName]

				if len(inputs[markdownNodeName]) > 0 {
					output += "\n\n---\n" + inputs[markdownNodeName] + "\n"
				}

				return output, nil
			},
			workflow.NodeConfig{},
		),
	})

	return &subgraph, nil
}
