package responses

import (
	"encoding/json"
	"fmt"
)

func ParseQueryOutput(body []byte) ([]Result, error) {
	qr := PrometheusQueryResult{}
	if err := json.Unmarshal(body, &qr); err != nil {
		return nil, fmt.Errorf("decoding Prometheus response: %w", err)
	}

	return qr.Data.Result, nil
}
