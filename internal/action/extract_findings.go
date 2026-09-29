package action

import (
	"fmt"
	"log/slog"
	"regexp"
	"time"

	"google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/agent/llmagent"
	"google.golang.org/adk/v2/workflow"
)

type ExtractFindingsFromCommentActionConfig struct {
	Model string `mapstructure:"model"`
}

type ExtractFindings struct {
	Name            string
	Config          *ExtractFindingsFromCommentActionConfig
	WorkflowContext *WorkflowContext
}

func (e *ExtractFindings) GetSubgraph() (*Subgraph, error) {
	// Extract the findings from an exiting forge comment.

	if e.WorkflowContext.Comment == nil {
		return &Subgraph{
			SubgraphItem{
				Name:      e.Name,
				DependsOn: nil,
				Node: workflow.NewFunctionNode(e.Name,
					func(_ agent.Context, _ any) (ReviewResult, error) {
						slog.Info("Empty comment, skipping extraction.")
						return ReviewResult{FindingsList: []Finding{}}, nil
					},
					workflow.NodeConfig{},
				),
			},
		}, nil
	}

	// Remove verification notes from comment so we do not prompt-inject ourselves
	matcher := regexp.MustCompile("(" + regexp.QuoteMeta(NOTES_FROM_VERIFICATION_PREFIX) + `[\s\S]*?)</details>`)
	redactedComment := matcher.ReplaceAllString(*e.WorkflowContext.Comment, "</details>")
	model, err := e.WorkflowContext.Config.GetModel(e.Config.Model)
	if err != nil {
		return nil, err
	}

	agent, err := llmagent.New(llmagent.Config{
		Name:              e.Name,
		Model:             model,
		Description:       "Extract Comment",
		GlobalInstruction: e.WorkflowContext.Config.Review.SystemPrompt,
		InstructionProvider: func(_ agent.ReadonlyContext) (string, error) {
			return fmt.Sprintf("# Comment\n\n```\n%s\n```\n # Your Task\n\nThe comment above contains findings from a code review in summary/details HTML tags. Extract the summaries and descriptions of all review findings in the text. \nIMPORTANT: Output PLAIN JSON only, no additional text, no Markdown wrappers, just plain parseable JSON in this format: {\"findings\": [ { \"summary\": \"Short headline summary of the finding.\", \"description\": \"Detailed description of the finding.\" } ] }\n", redactedComment), nil
		},
	})
	if err != nil {
		return nil, err
	}

	node, err := workflow.NewAgentNodeTyped[string, ReviewResult](
		agent, workflow.NodeConfig{
			Timeout:     time.Minute * 5,
			RetryConfig: workflow.DefaultRetryConfig(),
		},
	)
	if err != nil {
		return nil, err
	}

	return &Subgraph{
		SubgraphItem{
			Name:      e.Name,
			DependsOn: nil,
			Node:      node,
		},
	}, nil
}
