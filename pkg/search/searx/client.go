package searx

import (
	"context"
	"encoding/json"
	"log/slog"
	"math/rand/v2"
	"net"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/bornholm/ghostwriter/pkg/search"
	searchEngine "github.com/bornholm/ghostwriter/pkg/search"
	"github.com/gocolly/colly"
	"github.com/pkg/errors"
)

type Client struct{}

const instancesURL = "https://searx.space/data/instances.json"

func (c *Client) getInstanceURL(query string, ignored ...string) (*url.URL, error) {
	res, err := http.Get(instancesURL)
	if err != nil {
		return nil, errors.WithStack(err)
	}

	defer res.Body.Close()

	decoder := json.NewDecoder(res.Body)

	var instances Instances
	if err := decoder.Decode(&instances); err != nil {
		return nil, errors.WithStack(err)
	}

	var bestInstance *Instance
	var bestURL string

	for url, inst := range instances.Instances {
		if slices.Contains(ignored, url) {
			continue
		}

		includeSearchEngine := true
		for seOperator, seName := range searchEnginesOperators {
			if strings.Contains(query, "!"+seOperator) {
				engine, included := inst.Engines[seName]
				if !included || engine.ErrorRate > 50 {
					includeSearchEngine = false
					break
				}
			}
		}
		if !includeSearchEngine {
			continue
		}

		if inst.HTTP.StatusCode != http.StatusOK || inst.NetworkType != "normal" || inst.Timing.SearchGo.SuccessPercentage < 80 {
			continue
		}

		if bestInstance == nil {
			bestURL = url
			bestInstance = &inst
			continue
		}

		if bestInstance.Timing.Search.All.Mean > inst.Timing.Search.All.Mean || len(bestInstance.Engines) < len(inst.Engines) {
			bestURL = url
			bestInstance = &inst
			continue
		}
	}

	if bestInstance == nil {
		return nil, errors.New("no available instance")
	}

	url, err := url.Parse(bestURL)
	if err != nil {
		return nil, errors.WithStack(err)
	}

	return url, nil
}

func (c *Client) Search(ctx context.Context, search string) ([]searchEngine.Result, error) {
	maxRetries := 3
	ignored := make([]string, 0)
	retries := 0
	for {
		serverURL, err := c.getInstanceURL(search, ignored...)
		if err != nil {
			return nil, errors.WithStack(err)
		}

		results, err := c.doSearch(ctx, serverURL, search)
		if err != nil {
			if retries >= maxRetries {
				return nil, errors.WithStack(err)
			}

			retries++
			ignored = append(ignored, serverURL.String())
			time.Sleep(time.Second * time.Duration(rand.Float64()))
			continue
		}

		if len(results) == 0 {
			ignored = append(ignored, serverURL.String())
			retries++
			continue
		}

		return results, nil
	}

}

func (c *Client) doSearch(ctx context.Context, serverURL *url.URL, search string) ([]searchEngine.Result, error) {
	searchURL := serverURL.JoinPath("/search")

	query := searchURL.Query()
	query.Set("q", search)
	query.Set("language", "fr")
	searchURL.RawQuery = query.Encode()

	slog.DebugContext(ctx, "executing search", slog.String("url", searchURL.String()))

	var results []searchEngine.Result

	collector := colly.NewCollector(
		colly.UserAgent("Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/135.0.0.0 Safari/537.36"),
	)

	collector.WithTransport(&http.Transport{
		Proxy: http.ProxyFromEnvironment,
		DialContext: (&net.Dialer{
			Timeout:   30 * time.Second,
			KeepAlive: 30 * time.Second,
			DualStack: true,
		}).DialContext,
		MaxIdleConns:          100,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
	})

	collector.OnHTML("html", func(h *colly.HTMLElement) {
		h.DOM.Find("head > link").Each(func(i int, s *goquery.Selection) {
			url := s.AttrOr("href", "")
			if strings.HasPrefix(url, "/client") && strings.HasSuffix(url, ".css") {
				h.Request.Visit(url)
			}
		})
	})

	collector.OnHTML("body", func(h *colly.HTMLElement) {
		h.DOM.Find(".result").Each(func(i int, s *goquery.Selection) {
			link := s.Find("h3 > a[href]")

			url := link.AttrOr("href", "")
			if url == "" {
				return
			}

			title := link.Text()
			if title == "" {
				return
			}

			description := strings.TrimSpace(s.Find(".content").Text())
			if description == "" {
				return
			}

			results = append(results, searchEngine.Result{
				Title:       title,
				Description: description,
				URL:         url,
			})
		})
	})

	collector.OnRequest(func(r *colly.Request) {
		r.Headers.Set("Accept-Language", "fr-FR,fr;q=0.9,en-US;q=0.8,en;q=0.7")
		r.Headers.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,image/avif,image/webp,image/apng,*/*;q=0.8,application/signed-exchange;v=b3;q=0.7")
		r.Headers.Set("Connection", "keep-alive")
		r.Headers.Set("Sec-Fetch-Mode", "navigate")
		r.Headers.Set("Sec-Fetch-Dest", "document")
		r.Headers.Set("sec-fetch-site", "none")
		r.Headers.Set("Pragma", "no-cache")
		r.Headers.Set("Cache-Control", "no-cache")
	})

	if err := collector.Visit(searchURL.String()); err != nil {
		return nil, errors.WithStack(err)
	}

	return results, nil
}

func NewClient() *Client {
	return &Client{}
}

var _ search.Client = &Client{}
