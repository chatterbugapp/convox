package do

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/convox/convox/pkg/common"
	"github.com/convox/convox/pkg/structs"
)

var sequenceTokens sync.Map

type elasticSearchResult struct {
	Hits struct {
		Hits []struct {
			Index  string `json:"_index"`
			Source struct {
				Log       string
				Stream    string
				Timestamp time.Time `json:"@timestamp"`
			} `json:"_source"`
		}
	}
}

func (p *Provider) Log(app, stream string, ts time.Time, message string) error {
	index := fmt.Sprintf("convox.%s.%s", p.Name, app)

	body := map[string]interface{}{
		"log":        fmt.Sprintf("%s\n", message),
		"stream":     stream,
		"@timestamp": ts.Format(time.RFC3339Nano),
	}

	data, err := json.Marshal(body)
	if err != nil {
		return err
	}

	if _, err := p.Elastic.Index(index, bytes.NewReader(data)); err != nil {
		return err
	}

	return nil
}

func (p *Provider) AppLogs(name string, opts structs.LogsOptions) (io.ReadCloser, error) {
	r, w := io.Pipe()

	go p.streamLogs(p.Context(), w, fmt.Sprintf("convox.%s.%s", p.Name, name), opts)

	return r, nil
}

func (p *Provider) SystemLogs(opts structs.LogsOptions) (io.ReadCloser, error) {
	return p.AppLogs("system", opts)
}

func (p *Provider) streamLogs(ctx context.Context, w io.WriteCloser, index string, opts structs.LogsOptions) {
	defer w.Close()

	since := time.Time{}

	if opts.Since != nil {
		since = time.Now().UTC().Add(*opts.Since * -1)
	}

	for {
		// check for closed writer
		if _, err := w.Write([]byte{}); err != nil {
			return
		}

		select {
		case <-ctx.Done():
			return
		default:
			body := map[string]interface{}{
				"query": map[string]interface{}{
					"range": map[string]interface{}{
						"@timestamp": map[string]interface{}{
							"gt": since.UTC().Format(time.RFC3339),
						},
					},
				},
			}

			data, err := json.Marshal(body)
			if err != nil {
				fmt.Printf("err: %+v\n", err)
				return
			}

			res, err := p.Elastic.Search(
				p.Elastic.Search.WithIndex(index),
				p.Elastic.Search.WithSize(5000),
				p.Elastic.Search.WithBody(bytes.NewReader(data)),
			)
			if err != nil {
				fmt.Printf("err: %+v\n", err)
				return
			}
			defer res.Body.Close()

			data, err = ioutil.ReadAll(res.Body)
			if err != nil {
				fmt.Printf("err: %+v\n", err)
				return
			}

			var sres elasticSearchResult

			if err := json.Unmarshal(data, &sres); err != nil {
				fmt.Printf("err: %+v\n", err)
				return
			}

			sort.Slice(sres.Hits.Hits, func(i, j int) bool {
				return sres.Hits.Hits[i].Source.Timestamp.Before(sres.Hits.Hits[j].Source.Timestamp)
			})

			for _, log := range sres.Hits.Hits {
				prefix := ""

				if common.DefaultBool(opts.Prefix, false) {
					prefix = fmt.Sprintf("%s %s ", log.Source.Timestamp.Format(time.RFC3339), strings.ReplaceAll(log.Source.Stream, ".", "/"))
				}

				fmt.Fprintf(w, "%s%s", prefix, log.Source.Log)

				since = log.Source.Timestamp
			}

			time.Sleep(1 * time.Second)
		}
	}
}
