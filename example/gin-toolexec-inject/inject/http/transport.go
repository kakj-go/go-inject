//inject:net/http/client.go
package http

import (
	"bytes"
	"fmt"
	"io"
	"io/ioutil"
)

type Response struct {
	Body io.ReadCloser
}

func Post(url, contentType string, body io.Reader) (resp *Response, err error) {
	if body != nil {
		bodys, _ := ioutil.ReadAll(body)
		fmt.Printf("req: %v \n", string(bodys))
	}
	defer func() {
		resp.Body = ioutil.NopCloser(bytes.NewReader([]byte(`{"message":"hello world change"}`)))
	}()
	return nil, nil
}
