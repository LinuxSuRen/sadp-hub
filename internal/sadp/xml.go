package sadp

import "encoding/xml"

// parseXML 宽松解析 XML（不校验根元素名）。
func parseXML(data []byte, v interface{}) error {
	return xml.Unmarshal(data, v)
}
