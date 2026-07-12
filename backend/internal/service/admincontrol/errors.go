package admincontrol

import "fmt"

func errNotConfigured(name string) error {
	return fmt.Errorf("%s service not configured", name)
}
