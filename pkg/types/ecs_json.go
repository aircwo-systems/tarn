package types

import (
	"bytes"
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"time"
)

// The ECS persistence structs predate the HTTP wire adapter and use
// time.Time's normal RFC3339 JSON representation. Responses use AWS epoch
// seconds, so accepting both forms keeps local response helpers and old state
// files readable while the API package remains responsible for wire encoding.

func decodeECSTime(raw json.RawMessage) (time.Time, error) {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		return time.Time{}, nil
	}
	if raw[0] == '"' {
		var value string
		if err := json.Unmarshal(raw, &value); err != nil {
			return time.Time{}, err
		}
		if value == "" {
			return time.Time{}, nil
		}
		parsed, err := time.Parse(time.RFC3339Nano, value)
		if err != nil {
			return time.Time{}, err
		}
		return parsed, nil
	}

	seconds, err := strconv.ParseFloat(string(raw), 64)
	if err != nil {
		return time.Time{}, fmt.Errorf("invalid ECS timestamp %q: %w", string(raw), err)
	}
	whole, fraction := math.Modf(seconds)
	return time.Unix(int64(whole), int64(fraction*float64(time.Second))).UTC(), nil
}

func (value *TaskDefinition) UnmarshalJSON(data []byte) error {
	type alias TaskDefinition
	aux := struct {
		*alias
		RegisteredAt json.RawMessage `json:"RegisteredAt"`
	}{alias: (*alias)(value)}
	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}
	registeredAt, err := decodeECSTime(aux.RegisteredAt)
	if err != nil {
		return err
	}
	value.RegisteredAt = registeredAt
	return nil
}

func (value *ECSService) UnmarshalJSON(data []byte) error {
	type alias ECSService
	aux := struct {
		*alias
		CreatedAt json.RawMessage `json:"CreatedAt"`
	}{alias: (*alias)(value)}
	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}
	createdAt, err := decodeECSTime(aux.CreatedAt)
	if err != nil {
		return err
	}
	value.CreatedAt = createdAt
	return nil
}

func (value *Task) UnmarshalJSON(data []byte) error {
	type alias Task
	aux := struct {
		*alias
		StartedAt json.RawMessage `json:"StartedAt"`
		StoppedAt json.RawMessage `json:"StoppedAt"`
		CreatedAt json.RawMessage `json:"CreatedAt"`
	}{alias: (*alias)(value)}
	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}
	startedAt, err := decodeECSTime(aux.StartedAt)
	if err != nil {
		return err
	}
	stoppedAt, err := decodeECSTime(aux.StoppedAt)
	if err != nil {
		return err
	}
	createdAt, err := decodeECSTime(aux.CreatedAt)
	if err != nil {
		return err
	}
	if bytes.Equal(bytes.TrimSpace(aux.StartedAt), []byte("null")) || len(bytes.TrimSpace(aux.StartedAt)) == 0 {
		value.StartedAt = nil
	} else {
		value.StartedAt = &startedAt
	}
	if bytes.Equal(bytes.TrimSpace(aux.StoppedAt), []byte("null")) || len(bytes.TrimSpace(aux.StoppedAt)) == 0 {
		value.StoppedAt = nil
	} else {
		value.StoppedAt = &stoppedAt
	}
	value.CreatedAt = createdAt
	return nil
}
