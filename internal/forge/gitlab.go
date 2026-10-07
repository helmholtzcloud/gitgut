package forge

import (
	"errors"
	"fmt"
	"slices"

	gitlab "gitlab.com/gitlab-org/api/client-go/v3"
)

type GitLabConfig struct {
	Host           string   `mapstructure:"host"`
	AccessToken    string   `mapstructure:"access_token"`
	OptedInUserIDs *[]int64 `mapstructure:"opted_in_user_ids"`
	// PostAsInternalNote posts review comments as GitLab "internal notes"
	// (visible only to project members) instead of regular, externally-visible
	// notes. Defaults to false.
	PostAsInternalNote bool `mapstructure:"post_as_internal_note"`
}

type GitLab struct {
	client             *gitlab.Client
	optedInUserIDs     *[]int64
	postAsInternalNote bool
}

func NewGitLabClient(host string, token string, optedInUserIDs *[]int64, postAsInternalNote bool) (*GitLab, error) {
	client, err := gitlab.NewClient(token, gitlab.WithBaseURL(fmt.Sprintf("https://%s/api/v4", host)))
	if err != nil {
		return nil, err
	}

	return &GitLab{
		client:             client,
		optedInUserIDs:     optedInUserIDs,
		postAsInternalNote: postAsInternalNote,
	}, nil
}

func (c *GitLab) getCurrentUserID() (*int64, error) {
	user, _, err := c.client.Users.CurrentUser()
	if err != nil {
		return nil, err
	}

	return &user.ID, nil
}

func (c *GitLab) tryGetReviewNote(projectID int64, mergeRequestID int64) (*gitlab.Note, error) {
	currentuserID, err := c.getCurrentUserID()
	if err != nil {
		return nil, err
	}

	created_at := "created_at"
	desc := "desc"

	notesPager := func(pageOptions gitlab.PaginationOptionFunc) ([]*gitlab.Note, *gitlab.Response, error) {
		return c.client.Notes.ListMergeRequestNotes(projectID, mergeRequestID, &gitlab.ListMergeRequestNotesOptions{
			OrderBy: &created_at,
			Sort:    &desc,
			PerPage: 100,
		}, pageOptions)
	}

	for note, err := range gitlab.Scan2(notesPager) {
		if err != nil {
			return nil, err
		}

		if note.Author.ID == *currentuserID {
			return note, nil
		}
	}

	return nil, nil
}

func (c *GitLab) TryGetReviewComment(projectID int64, mergeRequestID int64) (*string, error) {
	note, err := c.tryGetReviewNote(projectID, mergeRequestID)
	if err != nil {
		return nil, err
	}

	if note == nil {
		return nil, nil
	}

	return &note.Body, err
}

func (c *GitLab) GetChangeMetadata(projectID int64, changeID int64) (*ChangeMetadata, error) {
	request, _, err := c.client.MergeRequests.GetMergeRequest(projectID, changeID, nil)
	if err != nil {
		return nil, err
	}

	if request == nil {
		return nil, errors.New("merge request not found")
	}

	return &ChangeMetadata{
		IsActive:            request.State == "opened",
		IsEligibleForReview: c.optedInUserIDs != nil && slices.Contains(*c.optedInUserIDs, request.Author.ID),
	}, nil
}

func (c *GitLab) GetRawDiffForChange(projectID int64, changeID int64) ([]byte, error) {
	diff, _, err := c.client.MergeRequests.ShowMergeRequestRawDiffs(projectID, changeID, nil)
	if err != nil {
		return nil, err
	}

	return diff, nil
}

func (c *GitLab) CreateOrUpdateReviewComment(projectID int64, changeID int64, comment string) error {
	note, err := c.tryGetReviewNote(projectID, changeID)
	if err != nil {
		return err
	}

	if note != nil {
		_, _, err = c.client.Notes.UpdateMergeRequestNote(projectID, changeID, note.ID, &gitlab.UpdateMergeRequestNoteOptions{
			Body: &comment,
		})
	} else {
		internal := c.postAsInternalNote
		_, _, err = c.client.Notes.CreateMergeRequestNote(projectID, changeID, &gitlab.CreateMergeRequestNoteOptions{
			Body:     &comment,
			Internal: &internal,
		})
	}

	if err != nil {
		return err
	}

	return nil
}

func (c *GitLab) DeleteReviewComment(projectID int64, changeID int64) error {
	note, err := c.tryGetReviewNote(projectID, changeID)
	if err != nil {
		return err
	}

	if note != nil {
		_, err = c.client.Notes.DeleteMergeRequestNote(projectID, changeID, note.ID)

		if err != nil {
			return err
		}
	}

	return nil
}

func (c *GitLab) GetArchiveForTargetRef(projectID int64, changeID int64) ([]byte, error) {
	mergeRequest, _, err := c.client.MergeRequests.GetMergeRequest(projectID, changeID, nil)
	if err != nil {
		return nil, err
	}

	format := "tar.gz"
	archive, _, err := c.client.Repositories.Archive(projectID, &gitlab.ArchiveOptions{
		Format: &format,
		SHA:    &mergeRequest.DiffRefs.BaseSha,
	})
	if err != nil {
		return nil, err
	}

	return archive, nil
}
