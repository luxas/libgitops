package content

import (
	"bytes"
	"fmt"
	"testing"

	"k8s.io/apimachinery/pkg/runtime/serializer/json"
	"sigs.k8s.io/yaml"
)

func TestTypePrint(t *testing.T) {
	t.Error(fmt.Printf("%T\n", bytes.NewBuffer(nil)))
	t.Error(fmt.Printf("%T\n", json.Framer.NewFrameReader(nil)))
}

const fooYAML = `

---

---
baz: 123
foo: bar
bar: true
---
foo: bar
bar: true

`

func TestFoo(t *testing.T) {
	//u, err := url.Parse("file:///foo/bar")
	/*u := &url.URL{
		//Scheme: "file",
		Path: ".",
	}
	t.Error(u, nil, u.RequestURI(), u.Host, u.Scheme)*/

	obj := map[string]interface{}{}

	err := yaml.UnmarshalStrict([]byte(fooYAML), &obj)
	t.Errorf("%+v %v", obj, err)
}
