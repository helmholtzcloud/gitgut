package action

import (
	"fmt"
	"slices"

	"github.com/go-viper/mapstructure/v2"
	"github.com/helmholtzcloud/gitgut/internal/config"
	"google.golang.org/adk/v2/workflow"
)

var ACTIONS_WITHOUT_OUTPUT = []string{"generate_comment"}

type WorkflowContext struct {
	Config       *config.Config
	Comment      *string
	Diff         string
	RepoTree     map[string]string
	ChangedFiles []string
}

type Finding struct {
	Summary     string `json:"summary" jsonschema:"The short headline summary of the code review finding."`
	Description string `json:"description" jsonschema:"The detailed description of the code review finding."`
}

type ReviewResult struct {
	FindingsList []Finding `json:"findings" jsonschema:"The list of findings the code review has yielded. One item per finding. Empty list if no findings."`
}

type VerifiedFinding struct {
	Finding
	NotesFromVerification string `json:"notes_from_verification,omitempty" jsonschema:"Notes from the verification process of the finding."`
}

type VerifiedReviewResult struct {
	FindingsList []VerifiedFinding `json:"findings" jsonschema:"The list of verified findings the code review has yielded. One item per finding. Empty list if no findings."`
}

type SubgraphItem struct {
	Name      string
	DependsOn *[]string
	Node      workflow.Node
}

type Subgraph []SubgraphItem

type Action interface {
	GetSubgraph() (*Subgraph, error)
}

func GetAction(name string, ac *config.ActionConfig, workflowContext *WorkflowContext) (Action, error) {
	switch ac.Action {
	case "extract_findings_from_comment":
		var extractFindingsFromCommentActionConfig ExtractFindingsFromCommentActionConfig
		err := mapstructure.Decode(ac.SpecificConfig, &extractFindingsFromCommentActionConfig)
		if err != nil {
			return nil, err
		}

		if ac.InputFrom != nil {
			return nil, fmt.Errorf("workflow step '%s' does not support 'input_from'", name)
		}

		if _, ok := workflowContext.Config.Models[extractFindingsFromCommentActionConfig.Model]; !ok {
			return nil, fmt.Errorf("workflow step '%s' references model '%s' which is not present in the configuration", name, extractFindingsFromCommentActionConfig.Model)
		}

		return &ExtractFindings{
			Name:            name,
			Config:          &extractFindingsFromCommentActionConfig,
			WorkflowContext: workflowContext,
		}, nil
	case "review":
		var reviewActionConfig ReviewActionConfig
		err := mapstructure.Decode(ac.SpecificConfig, &reviewActionConfig)
		if err != nil {
			return nil, err
		}

		if ac.InputFrom != nil {
			return nil, fmt.Errorf("workflow step '%s' does not support 'input_from'", name)
		}

		if _, ok := workflowContext.Config.Models[reviewActionConfig.Model]; !ok {
			return nil, fmt.Errorf("workflow step '%s' references model '%s' which is not present in the configuration", name, reviewActionConfig.Model)
		}

		return &Review{
			Name:            name,
			Config:          &reviewActionConfig,
			WorkflowContext: workflowContext,
		}, nil
	case "multi_review":
		var multiReviewActionConfig MultiReviewActionConfig
		err := mapstructure.Decode(ac.SpecificConfig, &multiReviewActionConfig)
		if err != nil {
			return nil, err
		}

		if ac.InputFrom != nil {
			return nil, fmt.Errorf("workflow step '%s' does not support 'input_from'", name)
		}

		if _, ok := workflowContext.Config.Models[multiReviewActionConfig.ReviewModel]; !ok {
			return nil, fmt.Errorf("workflow step '%s' references model '%s' which is not present in the configuration", name, multiReviewActionConfig.ReviewModel)
		}

		if _, ok := workflowContext.Config.Models[multiReviewActionConfig.AggregationModel]; !ok {
			return nil, fmt.Errorf("workflow step '%s' references model '%s' which is not present in the configuration", name, multiReviewActionConfig.AggregationModel)
		}

		return &MultiReview{
			Name:            name,
			Config:          &multiReviewActionConfig,
			WorkflowContext: workflowContext,
		}, nil
	case "filter_findings":
		var filterFindingsActionConfig FilterFindingsActionConfig
		err := mapstructure.Decode(ac.SpecificConfig, &filterFindingsActionConfig)
		if err != nil {
			return nil, err
		}

		if ac.InputFrom == nil || len(*ac.InputFrom) == 0 {
			return nil, fmt.Errorf("workflow step '%s' requires 'input_from'", name)
		} else {
			for _, step := range *ac.InputFrom {
				if slices.Contains(ACTIONS_WITHOUT_OUTPUT, step) {
					return nil, fmt.Errorf("workflow step '%s' requires 'input_from' from '%s', but '%s' does not return any output to be processed", name, step, step)
				}
			}
		}

		if _, ok := workflowContext.Config.Models[filterFindingsActionConfig.Model]; !ok {
			return nil, fmt.Errorf("workflow step '%s' references model '%s' which is not present in the configuration", name, filterFindingsActionConfig.Model)
		}

		return &FilterFindings{
			Name:            name,
			Config:          &filterFindingsActionConfig,
			WorkflowContext: workflowContext,
		}, nil
	case "verify_findings":
		var verifyFindingsActionConfig VerifyFindingsActionConfig
		err := mapstructure.Decode(ac.SpecificConfig, &verifyFindingsActionConfig)
		if err != nil {
			return nil, err
		}

		if ac.InputFrom == nil || len(*ac.InputFrom) == 0 {
			return nil, fmt.Errorf("workflow step '%s' requires 'input_from'", name)
		} else {
			for _, step := range *ac.InputFrom {
				if slices.Contains(ACTIONS_WITHOUT_OUTPUT, step) {
					return nil, fmt.Errorf("workflow step '%s' requires 'input_from' from '%s', but '%s' does not return any output to be processed", name, step, step)
				}
			}
		}

		if _, ok := workflowContext.Config.Models[verifyFindingsActionConfig.Model]; !ok {
			return nil, fmt.Errorf("workflow step '%s' references model '%s' which is not present in the configuration", name, verifyFindingsActionConfig.Model)
		}

		return &VerifyFindings{
			Name:            name,
			Config:          &verifyFindingsActionConfig,
			WorkflowContext: workflowContext,
		}, nil
	case "generate_comment":
		var commentActionConfig GenerateCommentActionConfig
		err := mapstructure.Decode(ac.SpecificConfig, &commentActionConfig)
		if err != nil {
			return nil, err
		}

		if ac.InputFrom == nil || len(*ac.InputFrom) == 0 {
			return nil, fmt.Errorf("workflow step '%s' requires 'input_from'", name)
		} else {
			for _, step := range *ac.InputFrom {
				if slices.Contains(ACTIONS_WITHOUT_OUTPUT, step) {
					return nil, fmt.Errorf("workflow step '%s' requires 'input_from' from '%s', but '%s' does not return any output to be processed", name, step, step)
				}
			}
		}

		if _, ok := workflowContext.Config.Models[commentActionConfig.SummaryModel]; !ok {
			return nil, fmt.Errorf("workflow step '%s' references model '%s' which is not present in the configuration", name, commentActionConfig.SummaryModel)
		}

		return &GenerateComment{
			Name:            name,
			Config:          &commentActionConfig,
			WorkflowContext: workflowContext,
		}, nil
	default:
		return nil, fmt.Errorf("unknown action block '%s'", ac.Action)
	}
}
