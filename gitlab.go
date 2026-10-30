package main

import (
	"fmt"

	gitlab "gitlab.com/gitlab-org/api/client-go"
)

//go:generate mockery --name GitLabClient
type GitLabClient interface {
	ListGroupProjects(groupID int64, options *gitlab.ListGroupProjectsOptions) ([]*gitlab.Project, *gitlab.Response, error)
	ListSubGroups(groupID int64, opt *gitlab.ListSubGroupsOptions, options ...gitlab.RequestOptionFunc) ([]*gitlab.Group, *gitlab.Response, error)
	ListProjectMergeRequests(projectID int64, options *gitlab.ListProjectMergeRequestsOptions) ([]*gitlab.BasicMergeRequest, *gitlab.Response, error)
	GetMergeRequestApprovalsConfiguration(projectID int64, mergeRequestID int64) (*gitlab.MergeRequestApprovals, *gitlab.Response, error)
}

type MergeRequestWithApprovals struct {
	MergeRequest *gitlab.BasicMergeRequest
	ApprovedBy   []string
	ProjectID    int64
}

type gitLabClient struct {
	client *gitlab.Client
}

func (c *gitLabClient) ListGroupProjects(groupID int64, options *gitlab.ListGroupProjectsOptions) ([]*gitlab.Project, *gitlab.Response, error) {
	return c.client.Groups.ListGroupProjects(groupID, options)
}

func (c *gitLabClient) ListSubGroups(groupID int64, opt *gitlab.ListSubGroupsOptions, options ...gitlab.RequestOptionFunc) ([]*gitlab.Group, *gitlab.Response, error) {
	return c.client.Groups.ListSubGroups(groupID, opt, options...)
}

func (c *gitLabClient) ListProjectMergeRequests(projectID int64, options *gitlab.ListProjectMergeRequestsOptions) ([]*gitlab.BasicMergeRequest, *gitlab.Response, error) {
	return c.client.MergeRequests.ListProjectMergeRequests(projectID, options)
}

func (c *gitLabClient) GetMergeRequestApprovalsConfiguration(projectID int64, mergeRequestID int64) (*gitlab.MergeRequestApprovals, *gitlab.Response, error) {
	return c.client.MergeRequestApprovals.GetConfiguration(projectID, mergeRequestID)
}

// newGitLabClient builds the wrapped GitLab client used by every entry point.
func newGitLabClient(config *Config) (GitLabClient, error) {
	glClient, err := gitlab.NewClient(config.GitLab.Token,
		gitlab.WithBaseURL(config.GitLab.URL))
	if err != nil {
		return nil, fmt.Errorf("error creating GitLab client: %w", err)
	}
	return &gitLabClient{client: glClient}, nil
}

func fetchOpenedMergeRequests(config *Config, client GitLabClient) ([]*MergeRequestWithApprovals, error) {
	var groupIDs []int64
	for _, group := range config.Groups {
		groupIDs = append(groupIDs, group.ID)

		// Add subgroups to the groups list.
		subgroupIDs, err := fetchSubGroups(group.ID, client)
		if err != nil {
			return nil, err
		}

		groupIDs = append(groupIDs, subgroupIDs...)
	}

	groupIDs = dedupeIDs(groupIDs)

	// Add projects from groups to the projects list.
	projectIDs, err := fetchProjectsFromGroups(groupIDs, client)
	if err != nil {
		return nil, err
	}

	for _, project := range config.Projects {
		projectIDs = append(projectIDs, project.ID)
	}

	projectIDs = dedupeIDs(projectIDs)

	var allMRs []*MergeRequestWithApprovals

	for _, projectID := range projectIDs {
		options := &gitlab.ListProjectMergeRequestsOptions{
			State:   gitlab.Ptr("opened"),
			OrderBy: gitlab.Ptr("updated_at"),
			Sort:    gitlab.Ptr("desc"),
			WIP:     gitlab.Ptr("no"),
			ListOptions: gitlab.ListOptions{
				PerPage: 50,
				Page:    1,
			},
		}

		for {
			mrs, resp, err := client.ListProjectMergeRequests(projectID, options)
			if err != nil {
				return nil, err
			}

			for _, mr := range mrs {
				approvals, _, err := client.GetMergeRequestApprovalsConfiguration(projectID, mr.IID)
				if err != nil {
					return nil, err
				}

				approvedBy := make([]string, 0, len(approvals.ApprovedBy))
				for _, approver := range approvals.ApprovedBy {
					if approver == nil || approver.User == nil {
						continue // 해석 불가한 승인자는 승인 집계에서 제외한다
					}
					approvedBy = append(approvedBy, approver.User.Name)
				}

				allMRs = append(allMRs, &MergeRequestWithApprovals{
					MergeRequest: mr,
					ApprovedBy:   approvedBy,
					ProjectID:    projectID,
				})
			}

			if resp.CurrentPage >= resp.TotalPages {
				break
			}

			options.Page = resp.NextPage
		}
	}

	return allMRs, nil
}

func fetchProjectsFromGroups(groupIDs []int64, client GitLabClient) ([]int64, error) {
	var projectIDs []int64
	for _, groupID := range groupIDs {
		options := &gitlab.ListGroupProjectsOptions{
			ListOptions: gitlab.ListOptions{
				PerPage: 50,
				Page:    1,
			},
		}

		for {
			projects, resp, err := client.ListGroupProjects(groupID, options)
			if err != nil {
				return nil, err
			}

			for _, project := range projects {
				projectIDs = append(projectIDs, project.ID)
			}

			if resp.CurrentPage >= resp.TotalPages {
				break
			}

			options.Page = resp.NextPage
		}
	}

	return projectIDs, nil
}

func fetchSubGroups(groupID int64, client GitLabClient) ([]int64, error) {
	var groupIDs []int64

	options := &gitlab.ListSubGroupsOptions{
		ListOptions: gitlab.ListOptions{
			PerPage: 50,
			Page:    1,
		},
	}

	for {
		groups, resp, err := client.ListSubGroups(groupID, options)
		if err != nil {
			return nil, err
		}

		for _, group := range groups {
			groupIDs = append(groupIDs, group.ID)
		}

		if resp.CurrentPage >= resp.TotalPages {
			break
		}

		options.Page = resp.NextPage
	}

	return groupIDs, nil
}

// dedupeIDs removes duplicate IDs while preserving first-seen order.
// A project reachable both through a configured group and through the
// projects list would otherwise be crawled twice, duplicating every
// notification and doubling the approvals API calls.
func dedupeIDs(ids []int64) []int64 {
	seen := make(map[int64]struct{}, len(ids))
	result := make([]int64, 0, len(ids))
	for _, id := range ids {
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		result = append(result, id)
	}
	return result
}
