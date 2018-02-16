// +build callable

package main

import (
	_ "github.com/docker/infrakit/pkg/callable/backend/http"
	_ "github.com/docker/infrakit/pkg/callable/backend/instance"
	_ "github.com/docker/infrakit/pkg/callable/backend/print"
	_ "github.com/docker/infrakit/pkg/callable/backend/sh"
	_ "github.com/docker/infrakit/pkg/callable/backend/ssh"
	_ "github.com/docker/infrakit/pkg/callable/backend/stack"
	_ "github.com/docker/infrakit/pkg/callable/backend/vmwscript"
)
