//inject:net/http/client.go
//inject:id http-client
package http

import "net/http"

type Client struct{}

func (client *Client) Do(req *http.Request) (resp *http.Response, err error) {
	if req != nil {
		req = req.Clone(req.Context())
		if req.Header == nil {
			req.Header = make(http.Header)
		}
		req.Header.Add("X-Go-Inject", "client")
	}
	defer func() {
		if resp != nil {
			if resp.Header == nil {
				resp.Header = make(http.Header)
			}
			resp.Header.Add("X-Go-Inject-Response", "client")
			resp.StatusCode = 299
			resp.Status = "299 Injected"
		}
	}()
	return nil, nil
}
