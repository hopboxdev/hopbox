package control

import (
	"encoding/json"
	"testing"
)

func TestRequestMarshal(t *testing.T) {
	req := Request{Command: "status"}
	data, err := json.Marshal(req)
	if err != nil {
		t.Fatal(err)
	}
	var decoded Request
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.Command != "status" {
		t.Errorf("command: got %q, want %q", decoded.Command, "status")
	}
}

func TestRequestDestroy(t *testing.T) {
	req := Request{Command: "destroy", Confirm: "mybox"}
	data, err := json.Marshal(req)
	if err != nil {
		t.Fatal(err)
	}
	var decoded Request
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.Confirm != "mybox" {
		t.Errorf("confirm: got %q, want %q", decoded.Confirm, "mybox")
	}
}

func TestResponseOK(t *testing.T) {
	resp := Response{OK: true, Data: map[string]string{"box": "default"}}
	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatal(err)
	}
	var decoded Response
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
	if !decoded.OK {
		t.Error("expected OK=true")
	}
	if decoded.Data["box"] != "default" {
		t.Errorf("data.box: got %q, want %q", decoded.Data["box"], "default")
	}
}

func TestResponseError(t *testing.T) {
	resp := Response{OK: false, Error: "not found"}
	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatal(err)
	}
	var decoded Response
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.OK {
		t.Error("expected OK=false")
	}
	if decoded.Error != "not found" {
		t.Errorf("error: got %q, want %q", decoded.Error, "not found")
	}
}
