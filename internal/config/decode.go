package config

import (
	"reflect"
	"time"

	"github.com/go-viper/mapstructure/v2"
)

// decoderConfig makes koanf parse duration strings such as "2s" into
// time.Duration and accept the loose string values that come from the
// environment (e.g. GOROKER_BROWSER_HEADLESS=true).
func decoderConfig(out any) *mapstructure.DecoderConfig {
	return &mapstructure.DecoderConfig{
		Result:           out,
		TagName:          "koanf",
		WeaklyTypedInput: true,
		DecodeHook: mapstructure.ComposeDecodeHookFunc(
			mapstructure.StringToTimeDurationHookFunc(),
			durationFromNumber,
		),
	}
}

// durationFromNumber treats a bare number as seconds so that
// `interval: 2` behaves like `interval: 2s` rather than 2 nanoseconds.
func durationFromNumber(from reflect.Type, to reflect.Type, data any) (any, error) {
	durationType := reflect.TypeOf(time.Duration(0))
	if to != durationType {
		return data, nil
	}
	// A value that is already a duration — from the defaults, or from the
	// string hook that ran before this one — is passed through untouched.
	if from == durationType {
		return data, nil
	}
	switch from.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return time.Duration(reflect.ValueOf(data).Int()) * time.Second, nil
	case reflect.Float32, reflect.Float64:
		return time.Duration(reflect.ValueOf(data).Float() * float64(time.Second)), nil
	default:
		return data, nil
	}
}
