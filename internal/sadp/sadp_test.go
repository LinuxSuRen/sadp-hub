package sadp

import (
	"testing"
)

func TestProbeMarshalUnmarshal(t *testing.T) {
	raw := MarshalProbe("u-1", "", "inquiry")
	p, err := UnmarshalProbe(raw)
	if err != nil {
		t.Fatalf("解析 Probe 失败: %v", err)
	}
	if p.Uuid != "u-1" || p.Types != "inquiry" {
		t.Fatalf("Probe 字段不符: %+v", p)
	}
}

func TestParseRealProbeMatch(t *testing.T) {
	raw := []byte(`<?xml version="1.0" encoding="UTF-8"?>
<ProbeMatch><Uuid>U1</Uuid>
<Types>inquiry</Types>
<SADPVersion>3.1.1</SADPVersion>
<DeviceType>137764</DeviceType>
<DeviceDescription>iDS-2ZMN2509N-HT</DeviceDescription>
<DeviceSN>iDS-20240407AARRFC5292335</DeviceSN>
<CommandPort>8000</CommandPort>
<HttpPort>80</HttpPort>
<MAC>04-03-12-98-9e-66</MAC>
<IPv4Address>192.168.18.108</IPv4Address>
<IPv4SubnetMask>255.255.254.0</IPv4SubnetMask>
<IPv4Gateway>192.168.18.1</IPv4Gateway>
<DHCP>true</DHCP>
<SoftwareVersion>V5.8.0build 230809</SoftwareVersion>
<Activated>true</Activated>
</ProbeMatch>`)
	dev, err := ParseResponse(raw)
	if err != nil {
		t.Fatalf("解析真实应答失败: %v", err)
	}
	if dev.SerialNO != "iDS-20240407AARRFC5292335" || dev.Model != "iDS-2ZMN2509N-HT" ||
		dev.SdkPort != 8000 || dev.HttpPort != 80 || !dev.Activated || !dev.DHCP {
		t.Fatalf("真实应答字段不符: %+v", dev)
	}
}

func TestParseAndMarshalSimulator(t *testing.T) {
	in := Device{
		Uuid: "u-2", DeviceType: "IPC", Name: "车间相机", MAC: "44:47:cc:00:00:01",
		SerialNO: "DS-IPC-0001", IPv4Address: "192.168.1.64",
		HttpPort: 80, SdkPort: 8000, Activated: true, Version: "5.7.13",
	}
	dev, err := ParseResponse(MarshalSimResponse(in))
	if err != nil {
		t.Fatalf("解析模拟应答失败: %v", err)
	}
	if dev.SerialNO != in.SerialNO || dev.IPv4Address != in.IPv4Address ||
		dev.SdkPort != in.SdkPort || !dev.Activated || dev.Name != in.Name {
		t.Fatalf("模拟应答字段不符: %+v", dev)
	}
}

func TestParseRejectsProbe(t *testing.T) {
	if _, err := ParseResponse(MarshalProbe("u-3", "", "")); err == nil {
		t.Fatal("Probe 不应被当作设备应答")
	}
}

func TestMatchKeyword(t *testing.T) {
	d := Device{SerialNO: "DS-IPC-0001", IPv4Address: "192.168.1.64", Name: "车间相机"}
	for _, kw := range []string{"ds-ipc", "192.168", "相机"} {
		if !matchKeyword(d, kw) {
			t.Fatalf("关键字 %q 应命中", kw)
		}
	}
	if matchKeyword(d, "nvr") {
		t.Fatal("关键字 nvr 不应命中")
	}
}
