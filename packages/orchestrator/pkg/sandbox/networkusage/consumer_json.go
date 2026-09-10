package networkusage

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strconv"
	"strings"
)

// Retained controls are closed schemas. Unlike ordinary json.Unmarshal, every
// non-optional field must be present, including false/zero identity fields.
// Numbers remain integer tokens and journal decimal strings remain exact.
func decodeConsumerJSON(raw []byte, target any) error {
	if err := checkJSON(raw); err != nil {
		return err
	}
	t := reflect.TypeOf(target)
	if t == nil || t.Kind() != reflect.Pointer {
		return errors.New("consumer decoder requires pointer")
	}
	if err := consumerShape(raw, t.Elem(), false); err != nil {
		return err
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	return d.Decode(target)
}

func consumerShape(raw []byte, t reflect.Type, stringInteger bool) error {
	if t.Kind() == reflect.Pointer {
		if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
			return nil
		}
		return consumerShape(raw, t.Elem(), stringInteger)
	}
	if stringInteger {
		var value string
		if err := json.Unmarshal(raw, &value); err != nil {
			return err
		}
		parsed, err := strconv.ParseUint(value, 10, 64)
		if err != nil || strconv.FormatUint(parsed, 10) != value {
			return errors.New("noncanonical journal integer")
		}
		return nil
	}
	switch t.Kind() {
	case reflect.Struct:
		var object map[string]json.RawMessage
		if err := json.Unmarshal(raw, &object); err != nil {
			return err
		}
		if object == nil {
			return errors.New("required retained object")
		}
		known := map[string]bool{}
		for i := 0; i < t.NumField(); i++ {
			field := t.Field(i)
			if !field.IsExported() {
				continue
			}
			tags := strings.Split(field.Tag.Get("json"), ",")
			name := tags[0]
			if name == "-" {
				continue
			}
			if name == "" {
				name = field.Name
			}
			known[name] = true
			optional, integer := false, false
			for _, tag := range tags[1:] {
				optional = optional || tag == "omitempty"
				integer = integer || tag == "string"
			}
			value, exists := object[name]
			if !exists {
				if optional {
					continue
				}
				return fmt.Errorf("missing retained field %s", name)
			}
			if err := consumerShape(value, field.Type, integer); err != nil {
				return fmt.Errorf("retained field %s: %w", name, err)
			}
		}
		for key := range object {
			if !known[key] {
				return fmt.Errorf("unknown retained field %s", key)
			}
		}
	case reflect.Slice:
		if t.Elem().Kind() == reflect.Uint8 { // JSON base64; decoder validates encoding.
			var data []byte
			return json.Unmarshal(raw, &data)
		}
		var values []json.RawMessage
		if err := json.Unmarshal(raw, &values); err != nil {
			return err
		}
		for _, value := range values {
			if err := consumerShape(value, t.Elem(), false); err != nil {
				return err
			}
		}
	default:
		if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
			return errors.New("null retained scalar")
		}
		value := reflect.New(t).Interface()
		return json.Unmarshal(raw, value)
	}
	return nil
}
