// Package health checks whether a service can respond to RPC requests.
package health

import "context"

type Health struct{}
type Request struct{}
type Response struct {
	Status string `json:"status"`
}

// Check reports RPC responsiveness. It does not call a model or external provider.
func (*Health) Check(_ context.Context, _ *Request, rsp *Response) error {
	rsp.Status = "ok"
	return nil
}
