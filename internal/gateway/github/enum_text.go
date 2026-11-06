package github

import "fmt"

func marshalEnum[T ~int](value T, values []string) ([]byte, error) {
	if value < 0 || int(value) >= len(values) || values[value] == "" {
		return nil, fmt.Errorf("unknown GitHub enum value %d", value)
	}
	return []byte(values[value]), nil
}

func unmarshalEnum[T ~int](text []byte, value *T, values map[string]T) error {
	decoded, ok := values[string(text)]
	if !ok {
		return fmt.Errorf("unknown %T: %q", *value, text)
	}
	*value = decoded
	return nil
}

func enumByText[T ~int](values []string) map[string]T {
	byText := make(map[string]T, len(values))
	for value, text := range values {
		if text != "" {
			byText[text] = T(value)
		}
	}
	return byText
}
