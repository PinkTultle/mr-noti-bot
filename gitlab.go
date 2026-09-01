package main

import (
	"fmt"

	"github.com/xanzy/go-gitlab"
)

//go:generate mockery --name GitLabClient
type GitLabClient interface {
	ListGroupProjects(groupID int, options *gitlab.ListGroupProjectsOptions) ([]*gitlab.Project, *gitlab.Response, error)
	ListSubGroups(groupID int, opt *gitlab.ListSubGroupsOptions, options ...gitlab.RequestOptionFunc) ([]*gitlab.Group, *gitlab.Response, error)
	ListProjectMergeRequests(projectID int, options *gitlab.ListProjectMergeRequestsOptions) ([]*gitlab.MergeRequest, *gitlab.Response, error)
	GetMergeRequestApprovalsConfiguration(projectID int, mergeRequestID int) (*gitlab.MergeRequestApprovals, *gitlab.Response, error)
}

type MergeRequestWithApprovals struct {
	MergeRequest *gitlab.MergeRequest
	ApprovedBy   []string
	ProjectID    int
}

type gitLabClient struct {
	client *gitlab.Client
}

func (c *gitLabClient) ListGroupProjects(groupID int, options *gitlab.ListGroupProjectsOptions) ([]*gitlab.Project, *gitlab.Response, error) {
	return c.client.Groups.ListGroupProjects(groupID, options)
}

func (c *gitLabClient) ListSubGroups(groupID int, opt *gitlab.ListSubGroupsOptions, options ...gitlab.RequestOptionFunc) ([]*gitlab.Group, *gitlab.Response, error) {
	return c.client.Groups.ListSubGroups(groupID, opt, options...)
}

func (c *gitLabClient) ListProjectMergeRequests(projectID int, options *gitlab.ListProjectMergeRequestsOptions) ([]*gitlab.MergeRequest, *gitlab.Response, error) {
	return c.client.MergeRequests.ListProjectMergeRequests(projectID, options)
}

func (c *gitLabClient) GetMergeRequestApprovalsConfiguration(projectID int, mergeRequestID int) (*gitlab.MergeRequestApprovals, *gitlab.Response, error) {
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
	var groupIDs []int
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
			State:   gitlab.String("opened"),
			OrderBy: gitlab.String("updated_at"),
			Sort:    gitlab.String("desc"),
			WIP:     gitlab.String("no"),
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

				approvedBy := make([]string, len(approvals.ApprovedBy))
				for i, approver := range approvals.ApprovedBy {
					approvedBy[i] = approver.User.Name
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

func fetchProjectsFromGroups(groupIDs []int, client GitLabClient) ([]int, error) {
	var projectIDs []int
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

func fetchSubGroups(groupID int, client GitLabClient) ([]int, error) {
	var groupIDs []int

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
func dedupeIDs(ids []int) []int {
	seen := make(map[int]struct{}, len(ids))
	result := make([]int, 0, len(ids))
	for _, id := range ids {
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		result = append(result, id)
	}
	return result
}
