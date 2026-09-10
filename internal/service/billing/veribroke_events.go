package billing

import "encoding/json"

const VeribrokeSTKRoutingKey = "veribroke.mpesa-stk"

type VeribrokeSTKRequest struct {
	RequestID   string            `json:"request_id"`
	PhoneNumber string            `json:"phone_number"`
	TransAmount int64             `json:"trans_amount"`
	TransDesc   string            `json:"trans_desc"`
	ServiceName string            `json:"service_name"`
	ReplyTo     string            `json:"reply_to"`
	Metadata    map[string]string `json:"metadata"`
}

type VeribrokeChargeResult struct {
	RequestID string          `json:"request_id"`
	Status    string          `json:"status"`
	Message   string          `json:"message,omitempty"`
	Metadata  json.RawMessage `json:"metadata,omitempty"`
}
