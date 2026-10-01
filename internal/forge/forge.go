package forge

import (
	"fmt"

	"github.com/go-viper/mapstructure/v2"
	"github.com/helmholtzcloud/gitgut/internal/config"
)

type ChangeMetadata struct {
	IsActive            bool
	IsEligibleForReview bool
}

type Forge interface {
	GetChangeMetadata(projectID int64, changeID int64) (*ChangeMetadata, error)
	GetRawDiffForChange(projectID int64, changeID int64) ([]byte, error)
	GetArchiveForTargetRef(projectID int64, changeID int64) ([]byte, error)
	TryGetReviewComment(projectID int64, changeID int64) (*string, error)
	CreateOrUpdateReviewComment(projectID int64, changeID int64, comment string) error
	DeleteReviewComment(projectID int64, changeID int64) error
}

func GetForge(fc *config.ForgeConfig) (Forge, error) {
	switch fc.Client {
	case "gitlab":
		var gitlabConfig GitLabConfig
		err := mapstructure.Decode(fc.SpecificConfig, &gitlabConfig)
		if err != nil {
			return nil, err
		}

		return NewGitLabClient(
			gitlabConfig.Host,
			gitlabConfig.AccessToken,
			gitlabConfig.OptedInUserIDs,
			gitlabConfig.PostAsInternalNote,
		)
	default:
		return nil, fmt.Errorf("unknown forge client '%s'", fc.Client)
	}
}
