package policy

import (
	"fmt"
	"reflect"
	"strings"

	"github.com/go-playground/validator/v10"
)

// validate runs the `validate:"..."` struct tags declared in command_gate.go. It
// is a package-level singleton because go-playground/validator caches
// struct metadata internally and is documented as safe for concurrent use
// once configured.
var validate = newValidator()

func newValidator() *validator.Validate {
	v := validator.New()
	// Report field paths using the yaml tag name (e.g. "spec.action")
	// instead of the Go field name (e.g. "Spec.Action"), so lint output
	// matches what's actually in the policy file.
	v.RegisterTagNameFunc(func(fld reflect.StructField) string {
		name := strings.SplitN(fld.Tag.Get("yaml"), ",", 2)[0]
		if name == "" || name == "-" {
			return fld.Name
		}
		return name
	})
	return v
}

// validateStruct runs struct-tag validation on p and reports every failure
// through c.
func validateStruct(c *IssueCollector, p any) {
	err := validate.Struct(p)
	if err == nil {
		return
	}

	verrs, ok := err.(validator.ValidationErrors)
	if !ok {
		c.Errorf("validation error: %v", err)
		return
	}

	for _, fe := range verrs {
		c.Errorf("%s: %s", fieldPath(fe), describeFailure(fe))
	}
}

// fieldPath strips the leading "CommandGatePolicy." root segment so messages read as
// "spec.action: ..." rather than "CommandGatePolicy.spec.action: ...".
func fieldPath(fe validator.FieldError) string {
	ns := fe.Namespace()
	if _, rest, ok := strings.Cut(ns, "."); ok {
		return rest
	}
	return ns
}

func describeFailure(fe validator.FieldError) string {
	switch fe.Tag() {
	case "required":
		return "must not be empty"
	case "required_without":
		return fmt.Sprintf("must be set when %s is not set", fe.Param())
	case "excluded_with":
		return fmt.Sprintf("must not be set together with %s", fe.Param())
	case "oneof":
		return fmt.Sprintf("must be one of [%s], got %q", fe.Param(), fe.Value())
	case "eq":
		return fmt.Sprintf("must equal %q, got %q", fe.Param(), fe.Value())
	case "gte":
		return fmt.Sprintf("must be >= %s, got %v", fe.Param(), fe.Value())
	case "min":
		return fmt.Sprintf("must have at least %s item(s)", fe.Param())
	default:
		return fmt.Sprintf("failed %q validation (value=%v)", fe.Tag(), fe.Value())
	}
}
