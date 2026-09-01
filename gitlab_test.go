package main

import (
	"testing"

	"github.com/flexoid/mergentle-reminder/mocks"
	"github.com/stretchr/testify/assert"
	mock "github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	gitlab "gitlab.com/gitlab-org/api/client-go"
)

func TestFetchOpenedMergeRequests(t *testing.T) {
	config := &Config{
		Projects: []ConfigProject{
			{ID: 1},
			{ID: 2},
		},
	}

	mockGitLabClient := mocks.NewGitLabClient(t)

	options := gitlab.ListProjectMergeRequestsOptions{
		State:   gitlab.Ptr("opened"),
		OrderBy: gitlab.Ptr("updated_at"),
		Sort:    gitlab.Ptr("desc"),
		WIP:     gitlab.Ptr("no"),
		ListOptions: gitlab.ListOptions{
			Page:    1,
			PerPage: 50,
		},
	}

	mockGitLabClient.On("ListProjectMergeRequests", config.Projects[0].ID, &options).Return(
		[]*gitlab.BasicMergeRequest{{IID: 1}},
		&gitlab.Response{CurrentPage: 1, NextPage: 2, TotalPages: 2},
		nil,
	).Once()

	optionsForPage2 := options
	optionsForPage2.Page = 2
	mockGitLabClient.On("ListProjectMergeRequests", config.Projects[0].ID, &optionsForPage2).Return(
		[]*gitlab.BasicMergeRequest{{IID: 2}},
		&gitlab.Response{CurrentPage: 2, TotalPages: 2},
		nil,
	).Once()

	mockGitLabClient.On("ListProjectMergeRequests", config.Projects[1].ID, &options).Return(
		[]*gitlab.BasicMergeRequest{{IID: 3}},
		&gitlab.Response{CurrentPage: 1, TotalPages: 1},
		nil,
	).Once()

	mockGitLabClient.On("GetMergeRequestApprovalsConfiguration", config.Projects[0].ID, int64(1)).Return(
		&gitlab.MergeRequestApprovals{
			ApprovedBy: []*gitlab.MergeRequestApproverUser{
				{User: &gitlab.BasicUser{Name: "John Doe"}},
				{User: &gitlab.BasicUser{Name: "Jane Doe"}},
			},
		},
		&gitlab.Response{},
		nil,
	).Once()

	mockGitLabClient.On("GetMergeRequestApprovalsConfiguration", mock.AnythingOfType("int64"), mock.AnythingOfType("int64")).Return(
		&gitlab.MergeRequestApprovals{
			ApprovedBy: []*gitlab.MergeRequestApproverUser{},
		},
		&gitlab.Response{},
		nil,
	).Twice()

	mrs, err := fetchOpenedMergeRequests(config, mockGitLabClient)

	mockGitLabClient.AssertExpectations(t)
	assert.NoError(t, err)
	assert.Equal(t, 3, len(mrs))

	assert.Equal(t, int64(1), mrs[0].MergeRequest.IID)
	assert.Equal(t, 2, len(mrs[0].ApprovedBy))
	assert.Equal(t, "John Doe", mrs[0].ApprovedBy[0])
	assert.Equal(t, "Jane Doe", mrs[0].ApprovedBy[1])

	assert.Equal(t, int64(2), mrs[1].MergeRequest.IID)
	assert.Equal(t, 0, len(mrs[1].ApprovedBy))

	assert.Equal(t, int64(3), mrs[2].MergeRequest.IID)
	assert.Equal(t, 0, len(mrs[2].ApprovedBy))
}

// TestFetchOpenedMergeRequests_NilApproverSkipped guards against a nil
// dereference on a GitLab approver we cannot resolve (e.g. a deleted user),
// and against the length of ApprovedBy growing past the number of approvers
// that were actually resolved (a pre-allocation bug that would otherwise
// leave blank entries and inflate the approval count used by classify.go).
func TestFetchOpenedMergeRequests_NilApproverSkipped(t *testing.T) {
	config := &Config{
		Projects: []ConfigProject{
			{ID: 1},
		},
	}

	mockGitLabClient := mocks.NewGitLabClient(t)

	options := gitlab.ListProjectMergeRequestsOptions{
		State:   gitlab.Ptr("opened"),
		OrderBy: gitlab.Ptr("updated_at"),
		Sort:    gitlab.Ptr("desc"),
		WIP:     gitlab.Ptr("no"),
		ListOptions: gitlab.ListOptions{
			Page:    1,
			PerPage: 50,
		},
	}

	mockGitLabClient.On("ListProjectMergeRequests", config.Projects[0].ID, &options).Return(
		[]*gitlab.BasicMergeRequest{{IID: 1}},
		&gitlab.Response{CurrentPage: 1, TotalPages: 1},
		nil,
	).Once()

	mockGitLabClient.On("GetMergeRequestApprovalsConfiguration", config.Projects[0].ID, int64(1)).Return(
		&gitlab.MergeRequestApprovals{
			ApprovedBy: []*gitlab.MergeRequestApproverUser{
				{User: &gitlab.BasicUser{Name: "John Doe"}},
				{User: nil},
				nil,
			},
		},
		&gitlab.Response{},
		nil,
	).Once()

	var mrs []*MergeRequestWithApprovals
	var err error
	require.NotPanics(t, func() {
		mrs, err = fetchOpenedMergeRequests(config, mockGitLabClient)
	})

	mockGitLabClient.AssertExpectations(t)
	require.NoError(t, err)
	require.Len(t, mrs, 1)
	assert.Equal(t, 1, len(mrs[0].ApprovedBy))
	assert.Equal(t, "John Doe", mrs[0].ApprovedBy[0])
}

func TestFetchProjectsFromGroups(t *testing.T) {
	groups := []int64{1, 2}

	mockGitLabClient := mocks.NewGitLabClient(t)

	options := gitlab.ListGroupProjectsOptions{
		ListOptions: gitlab.ListOptions{
			PerPage: 50,
			Page:    1,
		},
	}

	mockGitLabClient.On("ListGroupProjects", groups[0], &options).Return(
		[]*gitlab.Project{
			{ID: 1},
			{ID: 2},
		},
		&gitlab.Response{CurrentPage: 1, NextPage: 2, TotalPages: 2},
		nil,
	).Once()

	optionsForPage2 := options
	optionsForPage2.Page = 2
	mockGitLabClient.On("ListGroupProjects", groups[0], &optionsForPage2).Return(
		[]*gitlab.Project{
			{ID: 3},
			{ID: 4},
		},
		&gitlab.Response{CurrentPage: 2, TotalPages: 2},
		nil,
	).Once()

	mockGitLabClient.On("ListGroupProjects", groups[1], &options).Return(
		[]*gitlab.Project{
			{ID: 5},
		},
		&gitlab.Response{CurrentPage: 1, TotalPages: 1},
		nil,
	).Once()

	projectIDs, err := fetchProjectsFromGroups(groups, mockGitLabClient)

	mockGitLabClient.AssertExpectations(t)
	assert.NoError(t, err)
	assert.Equal(t, 5, len(projectIDs))

	assert.Equal(t, int64(1), projectIDs[0])
	assert.Equal(t, int64(2), projectIDs[1])
	assert.Equal(t, int64(3), projectIDs[2])
	assert.Equal(t, int64(4), projectIDs[3])
	assert.Equal(t, int64(5), projectIDs[4])
}

func TestFetchSubGroups(t *testing.T) {
	testCases := []struct {
		name        string
		mockClient  func() *mocks.GitLabClient
		groupID     int64
		expectedIDs []int64
		expectedErr error
	}{
		{
			name: "Single page of subgroups",
			mockClient: func() *mocks.GitLabClient {
				mockClient := mocks.NewGitLabClient(t)
				groups := []*gitlab.Group{
					{ID: 1},
					{ID: 2},
				}
				mockClient.On("ListSubGroups", int64(1), &gitlab.ListSubGroupsOptions{
					ListOptions: gitlab.ListOptions{
						PerPage: 50,
						Page:    1,
					},
				}).Return(groups, &gitlab.Response{TotalPages: 1, CurrentPage: 1}, nil)
				return mockClient
			},
			groupID:     1,
			expectedIDs: []int64{1, 2},
			expectedErr: nil,
		},
		{
			name: "Multiple pages of subgroups",
			mockClient: func() *mocks.GitLabClient {
				mockClient := mocks.NewGitLabClient(t)
				groupsPage1 := []*gitlab.Group{
					{ID: 1},
					{ID: 2},
				}
				groupsPage2 := []*gitlab.Group{
					{ID: 3},
					{ID: 4},
				}
				mockClient.On("ListSubGroups", int64(1), &gitlab.ListSubGroupsOptions{
					ListOptions: gitlab.ListOptions{
						PerPage: 50,
						Page:    1,
					},
				}).Return(groupsPage1, &gitlab.Response{TotalPages: 2, CurrentPage: 1, NextPage: 2}, nil)
				mockClient.On("ListSubGroups", int64(1), &gitlab.ListSubGroupsOptions{
					ListOptions: gitlab.ListOptions{
						PerPage: 50,
						Page:    2,
					},
				}).Return(groupsPage2, &gitlab.Response{TotalPages: 2, CurrentPage: 2}, nil)
				return mockClient
			},
			groupID:     1,
			expectedIDs: []int64{1, 2, 3, 4},
			expectedErr: nil,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			client := tc.mockClient()
			groupIDs, err := fetchSubGroups(tc.groupID, client)

			assert.Equal(t, tc.expectedErr, err)
			assert.Equal(t, tc.expectedIDs, groupIDs)
		})
	}
}

func TestDedupeIDs(t *testing.T) {
	assert.Equal(t, []int64{1, 2, 3}, dedupeIDs([]int64{1, 2, 1, 3, 2}))
	assert.Empty(t, dedupeIDs(nil))
}

func TestFetchOpenedMergeRequests_NoDuplicateWhenProjectAlsoInGroup(t *testing.T) {
	config := &Config{
		Groups:   []ConfigGroup{{ID: 1}},
		Projects: []ConfigProject{{ID: 77}}, // 그룹 1에 속한 프로젝트
	}

	mockClient := mocks.NewGitLabClient(t)
	mockClient.On("ListSubGroups", int64(1), mock.Anything).
		Return([]*gitlab.Group{}, &gitlab.Response{CurrentPage: 1, TotalPages: 1}, nil)
	mockClient.On("ListGroupProjects", int64(1), mock.Anything).
		Return([]*gitlab.Project{{ID: 77}}, &gitlab.Response{CurrentPage: 1, TotalPages: 1}, nil)
	// 프로젝트 77은 정확히 한 번만 조회되어야 한다
	mockClient.On("ListProjectMergeRequests", int64(77), mock.Anything).
		Return([]*gitlab.BasicMergeRequest{{IID: 5, Title: "MR"}},
			&gitlab.Response{CurrentPage: 1, TotalPages: 1}, nil).Once()
	mockClient.On("GetMergeRequestApprovalsConfiguration", int64(77), int64(5)).
		Return(&gitlab.MergeRequestApprovals{}, &gitlab.Response{}, nil).Once()

	mrs, err := fetchOpenedMergeRequests(config, mockClient)
	require.NoError(t, err)
	assert.Len(t, mrs, 1, "같은 프로젝트가 그룹과 projects 양쪽에 있어도 MR은 한 번만 나와야 한다")
	mockClient.AssertExpectations(t)
}
