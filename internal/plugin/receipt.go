package plugin

import (
	"context"
	"fmt"
	"net/url"
	"strings"
)

const Aria2DeliveryVersion = "co.delivery.aria2.v1"

// ReceiptHandler opts in to independent-output delivery. Submission and original
// receipt lookup are separate: an absent receipt never authorizes resubmission.
type ReceiptHandler interface {
	Handler
	DeliveryVersion() string
	DeliveryDestination() string
	DeliveryIdentity(Task) string
	SubmitDelivery(context.Context, Task) (any, string, error)
	QueryDelivery(context.Context, Task, string) (any, string, error)
}

func ValidateAria2Delivery(input map[string]any) error {
	for key := range input {
		if key != "url" && key != "dir" {
			return fmt.Errorf("交付输入仅支持url及可选dir")
		}
	}
	raw, ok := input["url"].(string)
	if !ok || strings.TrimSpace(raw) == "" || len(raw) > 8192 {
		return fmt.Errorf("url需要8192字节内的非空标量字符串")
	}
	u, e := url.Parse(strings.TrimSpace(raw))
	if e != nil || u.User != nil {
		return fmt.Errorf("下载地址无效或包含裸凭据")
	}
	switch strings.ToLower(u.Scheme) {
	case "http", "https":
		if u.Hostname() == "" {
			return fmt.Errorf("下载地址缺少主机")
		}
	case "magnet":
		if u.Query().Get("xt") == "" {
			return fmt.Errorf("magnet地址缺少xt")
		}
	default:
		return fmt.Errorf("下载地址仅支持http/https/magnet")
	}
	if v, present := input["dir"]; present {
		s, ok := v.(string)
		if !ok || strings.TrimSpace(s) == "" || len(s) > 1024 {
			return fmt.Errorf("dir需要1024字节内的非空字符串")
		}
	}
	return nil
}
