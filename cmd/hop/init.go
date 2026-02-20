package main

import (
	"fmt"
	"os"
)

// InitCmd generates a hopbox.yaml scaffold.
type InitCmd struct{}

func (c *InitCmd) Run() error {
	scaffold := `name: myapp
host: ""

services:
  app:
    type: docker
    image: myapp:latest
    ports: [8080]

bridges:
  - type: clipboard

session:
  manager: zellij
  name: myapp
`
	path := "hopbox.yaml"
	if _, err := os.Stat(path); err == nil {
		return fmt.Errorf("hopbox.yaml already exists")
	}
	return os.WriteFile(path, []byte(scaffold), 0644)
}
