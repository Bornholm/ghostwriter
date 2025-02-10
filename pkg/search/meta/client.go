package meta

import (
	"context"
	"sync"

	se "github.com/bornholm/ghostwriter/pkg/search"
	"github.com/hashicorp/go-multierror"
	"github.com/pkg/errors"
)

type Client struct {
	clients []se.Client
}

// Search implements search.Engine.
func (s *Client) Search(ctx context.Context, search string) ([]se.Result, error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	results := make(chan se.Result)

	var errLock sync.Mutex
	var aggregatedErr error

	defer close(results)

	var wg sync.WaitGroup

	wg.Add(len(s.clients))

	for _, e := range s.clients {
		go func(engine se.Client) {
			defer wg.Done()

			engineResults, err := engine.Search(ctx, search)
			if err != nil {
				errLock.Lock()
				aggregatedErr = multierror.Append(aggregatedErr, errors.WithStack(err))
				errLock.Unlock()
				return
			}

			for _, r := range engineResults {
				results <- r
			}
		}(e)
	}

	mergedResults := make([]se.Result, 0)
	go func() {
		resultSet := make(map[string]struct{})
		for r := range results {
			if _, exists := resultSet[r.URL]; exists {
				continue
			}

			mergedResults = append(mergedResults, r)
			resultSet[r.URL] = struct{}{}
		}
	}()

	wg.Wait()

	if aggregatedErr != nil {
		return mergedResults, aggregatedErr
	}

	return mergedResults, nil
}

func NewClient(clients ...se.Client) *Client {
	return &Client{
		clients: clients,
	}
}

var _ se.Client = &Client{}
