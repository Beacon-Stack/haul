package v1

import (
	"context"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"github.com/beacon-stack/haul/internal/core/category"
)

type categoryInput struct {
	Body category.Category
}

type deleteCategoryInput struct {
	Name string `path:"name" doc:"Category name"`
}

type categoryOutput struct {
	Body *category.Category
}

type categoryListOutput struct {
	Body []category.Category
}

// RegisterCategoryRoutes registers /api/v1/categories endpoints.
func RegisterCategoryRoutes(api huma.API, svc *category.Service) {
	huma.Register(api, huma.Operation{
		OperationID: "list-categories",
		Method:      http.MethodGet,
		Path:        "/api/v1/categories",
		Summary:     "List all categories",
		Tags:        []string{"Categories"},
	}, func(_ context.Context, _ *struct{}) (*categoryListOutput, error) {
		cats, err := svc.List()
		if err != nil {
			return nil, huma.Error500InternalServerError(err.Error())
		}
		if cats == nil {
			cats = []category.Category{}
		}
		return &categoryListOutput{Body: cats}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "create-category",
		Method:      http.MethodPost,
		Path:        "/api/v1/categories",
		Summary:     "Create a category",
		Tags:        []string{"Categories"},
	}, func(_ context.Context, input *categoryInput) (*categoryOutput, error) {
		if input.Body.Name == "" {
			return nil, huma.Error422UnprocessableEntity("name is required")
		}
		if err := svc.Create(input.Body); err != nil {
			return nil, huma.Error422UnprocessableEntity(err.Error())
		}
		return &categoryOutput{Body: &input.Body}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "update-category",
		Method:      http.MethodPut,
		Path:        "/api/v1/categories/{name}",
		Summary:     "Update a category",
		Tags:        []string{"Categories"},
	}, func(_ context.Context, input *struct {
		Name string `path:"name" doc:"Category name"`
		Body category.Category
	}) (*categoryOutput, error) {
		if err := svc.Update(input.Name, input.Body); err != nil {
			return nil, huma.Error404NotFound(err.Error())
		}
		cat, _ := svc.Get(input.Name)
		return &categoryOutput{Body: cat}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "delete-category",
		Method:      http.MethodDelete,
		Path:        "/api/v1/categories/{name}",
		Summary:     "Delete a category",
		Tags:        []string{"Categories"},
	}, func(_ context.Context, input *deleteCategoryInput) (*emptyOutput, error) {
		if err := svc.Delete(input.Name); err != nil {
			return nil, huma.Error404NotFound(err.Error())
		}
		return &emptyOutput{}, nil
	})
}
