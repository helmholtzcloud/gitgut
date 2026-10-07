package action

import (
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"time"

	"google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/agent/llmagent"
	"google.golang.org/adk/v2/session"
	"google.golang.org/adk/v2/tool"
	"google.golang.org/adk/v2/tool/functiontool"
	"google.golang.org/adk/v2/workflow"
)

type VerifyFindingsActionConfig struct {
	Model  string `mapstructure:"model"`
	Prompt string `mapstructure:"prompt"`
}

type VerifyFindings struct {
	Name            string
	Config          *VerifyFindingsActionConfig
	WorkflowContext *WorkflowContext
}

type VerificationResult struct {
	IsValid             bool   `json:"is_valid" jsonschema:"Whether this finding is valid."`
	VerificationSummary string `json:"verification_summary" jsonschema:"Evidence for the finding's validity."`
}

type readFileArgs struct {
	Path string `json:"path" jsonschema:"The path to the file to read from the File Tree."`
}

type readFileResults struct {
	Contents string `json:"contents" jsonschema:"The contents of the file."`
}

func (v *VerifyFindings) GetSubgraph() (*Subgraph, error) {
	// Conduct a multi review

	fileTree := "## File Tree\n\n"

	paths := make([]string, 0)
	for file := range v.WorkflowContext.RepoTree {
		paths = append(paths, file)
	}
	slices.Sort(paths)

	for _, file := range paths {
		fileTree += fmt.Sprintf("%s\n", file)
	}

	guidelines := "## Project Guidelines\n\n"
	changedFiles := "## Current File Contents\n\n"

	for filePath, fileContents := range v.WorkflowContext.RepoTree {
		if slices.Contains(v.WorkflowContext.Config.Review.GuidelinePaths, filePath) {
			guidelines += fmt.Sprintf("### %s\n\n```\n%s\n```\n\n", filePath, fileContents)
		} else if slices.Contains(v.WorkflowContext.ChangedFiles, filePath) {
			changedFiles += fmt.Sprintf("### %s\n\n```\n%s\n```\n\n", filePath, fileContents)
		}
	}

	reviewContext := fmt.Sprintf("# Context\n\n%s\n\n%s\n\n%s\n\n## Proposed Change (Diff)\n\n```\n%s```\n\n\n", fileTree, guidelines, changedFiles, v.WorkflowContext.Diff)

	verifierSchedulerNode := workflow.NewDynamicNode(v.Name,
		func(ctx agent.Context, reviewResult ReviewResult, _ func(*session.Event) error) (VerifiedReviewResult, error) {
			verifiedFindings := VerifiedReviewResult{FindingsList: make([]VerifiedFinding, 0)}

			slog.Info("Verifying findings", "unverified_count", len(reviewResult.FindingsList))

			for i, finding := range reviewResult.FindingsList {
				slog.Info("Starting verification")

				instruction := fmt.Sprintf("%s\n\n # Review Finding\n\n ## %s\n\n %s\n\n---\n\n # Your Task\n\n %s\n You may read files from the File Tree section using the read_file tool. DO NOT attempt re-read files listed under Current File Contents, you already have the contents in your context, there is no point in reading them again.\n\nOnce you're done, you MUST your verdict using the submit_result tool.\n\n", reviewContext, finding.Summary, finding.Description, v.Config.Prompt)

				readFiles := make(map[string]struct{})

				readFile := func(ctx agent.Context, input readFileArgs) (readFileResults, error) {
					if slices.Contains(v.WorkflowContext.ChangedFiles, input.Path) {
						slog.Warn("Agent requested file already present in context", "path", input.Path)
						return readFileResults{}, fmt.Errorf("file contents listed under Current File Contents. Do not attempt to read it again using the read_file tool")
					} else if _, ok := readFiles[input.Path]; ok {
						slog.Warn("Agent attempted to re-read file.", "path", input.Path)
						return readFileResults{}, fmt.Errorf("you already read this file. Look it up in your context. Do not attempt to read it again using the read_file tool")
					} else if contents, ok := v.WorkflowContext.RepoTree[input.Path]; ok {
						slog.Info("Agent reads file", "path", input.Path)
						readFiles[input.Path] = struct{}{}

						return readFileResults{Contents: contents}, nil
					} else {
						slog.Warn("Agent requested to read file which does not exist", "path", input.Path)
						return readFileResults{}, fmt.Errorf("file path not found in File Tree")
					}
				}

				readFileTool, err := functiontool.New(
					functiontool.Config{
						Name:        "read_file",
						Description: "Read a full file from the File Tree list.",
					},
					readFile)
				if err != nil {
					return verifiedFindings, err
				}

				var verificationResult *VerificationResult

				submitResult := func(ctx agent.Context, input VerificationResult) (string, error) {
					verificationResult = &input

					return "ok", err
				}

				submitResultTool, err := functiontool.New(
					functiontool.Config{
						Name:        "submit_result",
						Description: "Submit the result of your evaluation once you're done. You MUST call this at the end!",
					},
					submitResult)
				if err != nil {
					return verifiedFindings, err
				}

				model, err := v.WorkflowContext.Config.GetModel(v.Config.Model)
				if err != nil {
					return verifiedFindings, err
				}

				verificationAgent, err := llmagent.New(llmagent.Config{
					Name:              fmt.Sprintf("gitgut/%s/%d", v.Name, i),
					Model:             model,
					Description:       "Verify a finding from the review",
					GlobalInstruction: v.WorkflowContext.Config.Review.SystemPrompt,
					Tools: []tool.Tool{
						readFileTool,
						submitResultTool,
					},
				})

				verificationNode, err := workflow.NewAgentNodeTyped[string, string](
					verificationAgent, workflow.NodeConfig{
						Timeout:     time.Minute * 5,
						RetryConfig: workflow.DefaultRetryConfig(),
					},
				)
				if err != nil {
					return verifiedFindings, err
				}

				_, err = workflow.RunNode[string](ctx, verificationNode, instruction)
				if err != nil {
					return verifiedFindings, err
				}

				if verificationResult == nil {
					return verifiedFindings, errors.New("agent did not submit a verification result")
				}

				if verificationResult.IsValid {
					slog.Info("Finding passed verification")

					verifiedFindings.FindingsList = append(verifiedFindings.FindingsList, VerifiedFinding{
						NotesFromVerification: verificationResult.VerificationSummary,
						Finding:               finding,
					})
				} else {
					slog.Info("Finding did not pass verification")
				}
			}

			slog.Info("Verification done", "verified_count", len(verifiedFindings.FindingsList))

			return verifiedFindings, nil
		},
		workflow.NodeConfig{
			RetryConfig: workflow.DefaultRetryConfig(),
		},
	)

	subgraph := make(Subgraph, 0)
	subgraph = append(subgraph, SubgraphItem{
		Name:      v.Name,
		DependsOn: v.WorkflowContext.Config.Review.Workflow[v.Name].InputFrom,
		Node:      verifierSchedulerNode,
	})

	return &subgraph, nil
}
