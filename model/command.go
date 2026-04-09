package model

type Command struct {
	Op             string `json:"op"`
	Key            string `json:"key"`
	Value          string `json:"value"`
	IdempotencyKey string `json:"idempotency_key"`
}
