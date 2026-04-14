package v1

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"github.com/beacon-stack/haul/internal/core/tag"
)

type tagListOutput struct {
	Body []string
}

type tagDeleteInput struct {
	Tag string `path:"tag" doc:"Tag name"`
}

// RegisterTagRoutes registers /api/v1/tags endpoints.
func RegisterTagRoutes(api huma.API, svc *tag.Service) {
	huma.Register(api, huma.Operation{
		OperationID: "list-tags",
		Method:      http.MethodGet,
		Path:        "/api/v1/tags",
		Summary:     "List all tags",
		Tags:        []string{"Tags"},
	}, func(_ context.Context, _ *struct{}) (*tagListOutput, error) {
		tags, err := svc.List()
		if err != nil {
			return nil, huma.Error500InternalServerError(err.Error())
		}
		if tags == nil {
			tags = []string{}
		}
		return &tagListOutput{Body: tags}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "delete-tag",
		Method:      http.MethodDelete,
		Path:        "/api/v1/tags/{tag}",
		Summary:     "Delete a tag from all torrents",
		Tags:        []string{"Tags"},
	}, func(_ context.Context, input *tagDeleteInput) (*emptyOutput, error) {
		if err := svc.DeleteTag(input.Tag); err != nil {
			return nil, huma.Error500InternalServerError(err.Error())
		}
		return &emptyOutput{}, nil
	})
}
