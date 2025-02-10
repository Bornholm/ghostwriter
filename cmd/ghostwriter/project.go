package main

import (
	"os"

	"github.com/bornholm/genai/llm/tool/index"
	"github.com/pkg/errors"
	"gopkg.in/yaml.v3"
)

type ProjectResource struct {
	Type     string `yaml:"type"`
	Resource string `yaml:"resource"`
}

type Project struct {
	Topic    string            `yaml:"topic"`
	Corpus   []ProjectResource `yaml:"corpus"`
	Language string            `yaml:"language"`
}

func parseProjectFile(filename string) (*Project, error) {
	data, err := os.ReadFile(filename)
	if err != nil {
		return nil, errors.WithStack(err)
	}

	var project Project
	if err := yaml.Unmarshal(data, &project); err != nil {
		return nil, errors.WithStack(err)
	}

	return &project, nil
}

func projectCorpusToResourceOptions(corpus []ProjectResource) ([]index.SearchOptionFunc, error) {
	resourceCollections := make([]index.ResourceCollection, 0)
	resources := make([]index.Resource, 0)

	for _, r := range corpus {
		switch r.Type {
		case "files":
			resourceCollections = append(resourceCollections, index.FileCollection(r.Resource))
		case "website":
			resourceCollections = append(resourceCollections, index.WebsiteCollection(r.Resource))
		case "url":
			resources = append(resources, index.URLResource(r.Resource))
		default:
			resources = append(resources, index.URLResource(r.Resource))
		}
	}

	return []index.SearchOptionFunc{
		index.WithResourceCollections(resourceCollections...),
		index.WithResources(resources...),
	}, nil
}
