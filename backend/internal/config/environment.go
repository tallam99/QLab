package config

import "fmt"

//go:generate go tool enumer -type=Environment -trimprefix=Env -transform=lower -output=environment_enumer.go

// Environment is the deployment environment the service runs in.
//
// Project convention for enums: values are named EnumName<Value> (e.g.
// EnvLocal), and the zero value is EnumNameUnknown, which is never valid —
// encountering it in a logical flow means a value was never set, i.e. a
// programmer error. String()/parse methods are generated (see go:generate).
type Environment int

const (
	EnvUnknown Environment = iota // zero value; never valid
	EnvLocal
	EnvStaging
	EnvProd
)

// Decode implements envconfig.Decoder so QLAB_ENV is parsed straight into the
// enum, rejecting anything that isn't a known, valid environment.
func (e *Environment) Decode(value string) error {
	parsed, err := EnvironmentString(value)
	if err != nil || parsed == EnvUnknown {
		return fmt.Errorf("invalid environment %q: must be one of local, staging, prod", value)
	}
	*e = parsed
	return nil
}
