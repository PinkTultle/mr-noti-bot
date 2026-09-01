module github.com/flexoid/mergentle-reminder

go 1.25

require (
	github.com/slack-go/slack v0.29.0
	github.com/stretchr/testify v1.12.1
	gopkg.in/yaml.v2 v2.4.0
)

require (
	github.com/aws/aws-lambda-go v1.54.0 // v1.55.0부터 go 1.26 요구 — go.mod/CI/Dockerfile을 또 올리기엔 이득(패치 1개)이 작아 보류
	github.com/reugn/go-quartz v0.15.2
	gitlab.com/gitlab-org/api/client-go v1.46.0
)

require (
	github.com/fatih/color v1.18.0 // indirect
	github.com/google/go-querystring v1.2.0 // indirect
	github.com/gorilla/websocket v1.5.3 // indirect
	github.com/hashicorp/go-cleanhttp v0.5.2 // indirect
	github.com/hashicorp/go-retryablehttp v0.7.8 // indirect
	github.com/stretchr/objx v0.5.3 // indirect
	go.yaml.in/yaml/v3 v3.0.5 // indirect
	golang.org/x/oauth2 v0.34.0 // indirect
	golang.org/x/time v0.14.0 // indirect
)
