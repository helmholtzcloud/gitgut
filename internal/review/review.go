package review

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"unicode/utf8"

	"github.com/helmholtzcloud/gitgut/internal/action"
	"github.com/helmholtzcloud/gitgut/internal/config"
	"github.com/helmholtzcloud/gitgut/internal/forge"
	"golang.org/x/text/message"
	"google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/agent/workflowagent"
	"google.golang.org/adk/v2/runner"
	"google.golang.org/adk/v2/workflow"
	"google.golang.org/genai"
)

// Hardcoded 10M token limit for now
const GLOBAL_INPUT_TOKEN_LIMIT = 10_000_000

func RunReview(projectID int64, changeID int64, dryRun bool) error {
	slog.SetLogLoggerLevel(slog.LevelDebug)

	config, err := config.LoadConfig()
	if err != nil {
		return err
	}

	forgeClient, err := forge.GetForge(&config.Forge)
	if err != nil {
		return err
	}

	slog.Debug("Loading change metadata...")
	metadata, err := forgeClient.GetChangeMetadata(projectID, changeID)
	if err != nil {
		return err
	}

	if !metadata.IsEligibleForReview {
		return errors.New("the change is not eligible for review (e.g. the author did not opt into reviews)")
	}

	slog.Debug("Checking for existing comment...")
	currentComment, err := forgeClient.TryGetReviewComment(projectID, changeID)
	if err != nil {
		return err
	}

	if !metadata.IsActive && !dryRun {
		if currentComment != nil {
			slog.Debug("Change is not active, deleting stale review...")
			err = forgeClient.DeleteReviewComment(projectID, changeID)
			if err != nil {
				return err
			}
		} else {
			slog.Debug("Change is not active and there is no old comment, nothing to do here.")
		}
	}

	repoTree := make(map[string]string)

	slog.Debug("Downloading diff...")
	diff, err := forgeClient.GetRawDiffForChange(projectID, changeID)
	if err != nil {
		return err
	}

	changedFiles := []string{}
	for _, line := range strings.Split(string(diff), "\n") {
		if strings.HasPrefix(line, "diff --git a/") {
			path, _ := strings.CutPrefix(strings.Split(line, " ")[2], "a/")
			changedFiles = append(changedFiles, path)
		}
	}

	slog.Debug("Downloading code...")
	codeArchive, err := forgeClient.GetArchiveForTargetRef(projectID, changeID)
	if err != nil {
		return err
	}

	slog.Debug("Extracting code...")
	gzipReader, err := gzip.NewReader(bytes.NewReader(codeArchive))
	if err != nil {
		return err
	}

	tarReader := tar.NewReader(gzipReader)
	for {
		file, err := tarReader.Next()
		if err != nil {
			if err == io.EOF {
				break
			}

			return err
		}

		if file.Typeflag == tar.TypeReg {
			localPath := file.Name[strings.Index(file.Name, "/")+1:]

			rawContent, err := io.ReadAll(tarReader)
			if err != nil {
				return err
			}

			textContent := string(rawContent)

			if !utf8.ValidString(textContent) {
				continue
			}

			repoTree[localPath] = textContent
		}
	}

	workflowContext := &action.WorkflowContext{
		Config:       config,
		Comment:      currentComment,
		Diff:         string(diff),
		RepoTree:     repoTree,
		ChangedFiles: changedFiles,
	}

	// Get nodes
	graphItems := make(map[string]*action.SubgraphItem)
	for actionName, actionConfig := range config.Review.Workflow {
		action, err := action.GetAction(actionName, &actionConfig, workflowContext)
		if err != nil {
			return err
		}

		subgraph, err := action.GetSubgraph()
		if err != nil {
			return err
		}

		for _, node := range *subgraph {
			graphItems[node.Name] = &node
		}
	}

	// Assemble graph
	startNode := workflow.Start

	workflowGraph := workflow.NewEdgeBuilder()
	for _, item := range graphItems {
		if item.DependsOn == nil || len(*item.DependsOn) == 0 {
			// Node does not depend on anything -> depends on start
			workflowGraph.Add(startNode, item.Node)
		} else {
			// Add dependencies
			for _, dependency := range *item.DependsOn {
				workflowGraph.Add(graphItems[dependency].Node, item.Node)
			}
		}
	}

	workflowAgent, err := workflowagent.New(
		workflowagent.Config{
			Name:        "gitgut_root",
			Description: "Run the GitGut review.",
			Edges:       workflowGraph.Build(),
		},
	)
	if err != nil {
		return err
	}

	startingPrompt := genai.NewContentFromText(
		"Perform the review.",
		genai.RoleUser,
	)

	workflowRunner, err := runner.NewInMemory("gitgut_runner", workflowAgent)
	if err != nil {
		return err
	}

	slog.Debug("Runnning review workflow...")
	events := workflowRunner.Run(
		context.Background(),
		fmt.Sprint(projectID),
		fmt.Sprint(changeID),
		startingPrompt,
		agent.RunConfig{
			StreamingMode: agent.StreamingModeSSE,
		},
	)

	var output string

	var inputTokens, thinkingTokens, outputTokens, cachedTokens uint64

	for event, err := range events {
		if err != nil {
			return err
		}

		if event.ErrorCode != "" || event.ErrorMessage != "" {
			slog.Error("LLM returned error", "node", event.NodeInfo.Path, "code", event.ErrorCode, "message", event.ErrorMessage)
		}

		if event.FinishReason != "" && event.FinishReason != genai.FinishReasonStop && event.FinishReason != genai.FinishReasonUnspecified {
			slog.Error("LLM returned non-stop/unspecified finish reason", "node", event.NodeInfo.Path, "reason", event.FinishReason)
		}

		if event.UsageMetadata != nil {
			cachedTokens += uint64(event.UsageMetadata.CachedContentTokenCount)
			inputTokens += uint64(event.UsageMetadata.PromptTokenCount)
			thinkingTokens += uint64(event.UsageMetadata.ThoughtsTokenCount)
			outputTokens += uint64(event.UsageMetadata.CandidatesTokenCount) - uint64(event.UsageMetadata.ThoughtsTokenCount)

			if inputTokens > GLOBAL_INPUT_TOKEN_LIMIT {
				slog.Error("Global input token limit reached", "inputTokens",
					inputTokens,
					"outputTokens",
					outputTokens,
					"thinkingTokens",
					thinkingTokens,
					"cachedTokens",
					cachedTokens,
				)

				return errors.New("global input token limit reached")
			}
		}

		if !event.Partial && event.IsFinalResponse() {
			slog.Debug("Workflow node finished",
				"node",
				event.NodeInfo.Path,
				"inputTokens",
				inputTokens,
				"outputTokens",
				outputTokens,
				"thinkingTokens",
				thinkingTokens,
				"cachedTokens",
				cachedTokens)

			rawOutput, ok := event.Output.(string)
			if ok {
				output = rawOutput
			}
		}
	}

	slog.Info("Finished review",
		"inputTokens",
		inputTokens,
		"outputTokens",
		outputTokens,
		"thinkingTokens",
		thinkingTokens,
		"cachedTokens",
		cachedTokens)

	numberPrinter := message.NewPrinter(message.MatchLanguage("en"))
	output += fmt.Sprintf("\n\n---\n\nToken Usage: Input %s / Output %s / Thinking %s / Cached %s\n", numberPrinter.Sprintf("%d", inputTokens), numberPrinter.Sprintf("%d", outputTokens), numberPrinter.Sprintf("%d", thinkingTokens), numberPrinter.Sprintf("%d", cachedTokens))

	if dryRun {
		slog.Info("Dry run result (no comment update posted)", "review", output)
	} else {
		slog.Info("Updating comment...")

		err := forgeClient.CreateOrUpdateReviewComment(projectID, changeID, output)
		if err != nil {
			return err
		}
	}

	slog.Info("Done.")

	return nil
}
