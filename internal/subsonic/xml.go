package subsonic

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"encoding/xml"
	"fmt"
)

// Derived from the official Subsonic 1.16.1 response schema. Attribute allowlists
// and child order keep the XML wire format conformant while JSON retains its own shape.
// Source: https://www.subsonic.org/pages/inc/api/schema/subsonic-rest-api-1.16.1.xsd
//
//go:embed xml_shapes.json
var xmlShapesJSON []byte

type xmlShape struct {
	Attrs    []string `json:"attrs"`
	Children []struct {
		Name string `json:"name"`
		Type string `json:"type"`
	} `json:"children"`
	Text bool `json:"text"`
}

var xmlShapes = func() map[string]xmlShape {
	var shapes map[string]xmlShape
	if err := json.Unmarshal(xmlShapesJSON, &shapes); err != nil {
		panic(err)
	}
	return shapes
}()

func encodeSchemaNode(e *xml.Encoder, name, typeName string, value any) error {
	raw, err := json.Marshal(value)
	if err != nil {
		return err
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var normalized any
	if err = decoder.Decode(&normalized); err != nil {
		return err
	}
	return encodeSchemaValue(e, name, typeName, normalized)
}
func encodeSchemaValue(e *xml.Encoder, name, typeName string, value any) error {
	if values, ok := value.([]any); ok {
		for _, item := range values {
			if err := encodeSchemaValue(e, name, typeName, item); err != nil {
				return err
			}
		}
		return nil
	}
	start := xml.StartElement{Name: xml.Name{Local: name}}
	shape, known := xmlShapes[typeName]
	if object, ok := value.(map[string]any); ok && known {
		if typeName == "Child" {
			if _, ok := object["title"]; !ok {
				if title, ok := object["name"]; ok {
					object["title"] = title
					object["isDir"] = true
				}
			}
		}
		for _, key := range shape.Attrs {
			if v, ok := object[key]; ok && v != nil {
				start.Attr = append(start.Attr, xml.Attr{Name: xml.Name{Local: key}, Value: fmt.Sprint(v)})
			}
		}
		if err := e.EncodeToken(start); err != nil {
			return err
		}
		if shape.Text {
			if text, ok := object["value"]; ok {
				if err := e.EncodeToken(xml.CharData(fmt.Sprint(text))); err != nil {
					return err
				}
			}
		}
		for _, child := range shape.Children {
			if content, ok := object[child.Name]; ok && content != nil {
				if err := encodeSchemaValue(e, child.Name, child.Type, content); err != nil {
					return err
				}
			}
		}
		return e.EncodeToken(start.End())
	}
	if err := e.EncodeToken(start); err != nil {
		return err
	}
	if value != nil {
		if err := e.EncodeToken(xml.CharData(fmt.Sprint(value))); err != nil {
			return err
		}
	}
	return e.EncodeToken(start.End())
}
