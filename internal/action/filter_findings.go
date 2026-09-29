package action

import (
	"fmt"
	"time"

	"google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/agent/llmagent"
	"google.golang.org/adk/v2/workflow"
)

type FilterFindingsActionConfig struct {
	Model  string `mapstructure:"model"`
	Prompt string `mapstructure:"string"`
}

type FilterFindings struct {
	Name            string
	Config          *FilterFindingsActionConfig
	WorkflowContext *WorkflowContext
}

func (f *FilterFindings) GetSubgraph() (*Subgraph, error) {
	subgraph := make(Subgraph, 0)

	joinNodeName := fmt.Sprintf("gitgut/%s/join", f.Name)
	joinNode := workflow.NewJoinNode(joinNodeName)

	subgraph = append(subgraph, SubgraphItem{
		Name:      joinNodeName,
		DependsOn: f.WorkflowContext.Config.Review.Workflow[f.Name].InputFrom,
		Node:      joinNode,
	})

	model, err := f.WorkflowContext.Config.GetModel(f.Config.Model)
	if err != nil {
		return nil, err
	}

	agent, err := llmagent.New(llmagent.Config{
		Name:              f.Name,
		Model:             model,
		Description:       "Filter Findings",
		GlobalInstruction: f.WorkflowContext.Config.Review.SystemPrompt,
		InstructionProvider: func(_ agent.ReadonlyContext) (string, error) {
			return fmt.Sprintf("# Your Task\n\n Filter the findings: %s\n It is okay if no findings meet the criteria, in that case provide an empty array (`[]`) in `findings`.\nIMPORTANT: Output PLAIN JSON only, no additional text, no Markdown wrappers, just plain parseable JSON in this format: {\"findings\": [ { \"summary\": \"Short headline summary of the finding.\", \"description\": \"Detailed description of the finding.\" } ] }\n", f.Config.Prompt), nil
		},
	})
	if err != nil {
		return nil, err
	}

	node, err := workflow.NewAgentNodeTyped[map[string]ReviewResult, ReviewResult](
		agent, workflow.NodeConfig{
			Timeout:     time.Minute * 5,
			RetryConfig: workflow.DefaultRetryConfig(),
		},
	)
	if err != nil {
		return nil, err
	}

	subgraph = append(subgraph, SubgraphItem{
		Name:      f.Name,
		DependsOn: &[]string{joinNodeName},
		Node:      node,
	})

	return &subgraph, nil
}
