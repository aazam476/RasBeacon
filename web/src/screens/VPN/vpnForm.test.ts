import { describe, expect, it } from "vitest";
import type { VPNSettings } from "../../api/types";
import { describeVPNState } from "../../shared/format";
import { fromForm, splitList, toForm } from "./vpnForm";

const saved: VPNSettings = {
  enabled: true,
  killSwitch: true,
  configured: true,
  addresses: ["10.66.0.2/32", "fd00:66::2/128"],
  dns: ["10.66.0.1"],
  mtu: 0,
  peerPublicKey: "xTIBA5rboUvnH4htodjb6e697QjLERt1NAB4mZqp8Dg=",
  endpoint: "vpn.example.com:51820",
  allowedIps: ["0.0.0.0/0", "::/0"],
  persistentKeepalive: 25,
  publicKey: "HIgo9xNzJMWLKASShiTqIybxZ0U3wGLiUeJ1PKf8ykw=",
  hasPresharedKey: false,
  routedIpv6Prefix: "2001:db8:1234:5678::/64",
};

describe("VPN form", () => {
  it("round-trips the settings", () => {
    expect(fromForm(toForm(saved), saved)).toEqual({ ...saved, privateKey: "", presharedKey: "" });
  });
  it("shows lists as comma-separated text and empty numbers as defaults", () => {
    const form = toForm(saved);
    expect(form.addresses).toBe("10.66.0.2/32, fd00:66::2/128");
    expect(form.mtu).toBe("");
    expect(fromForm({ ...form, mtu: "", persistentKeepalive: "" }, saved)).toMatchObject({
      mtu: 0,
      persistentKeepalive: 0,
    });
  });
  it("suggests sending everything through the tunnel for a new VPN", () => {
    expect(toForm({ ...saved, allowedIps: [] }).allowedIps).toBe("0.0.0.0/0, ::/0");
  });
  it("splits lists", () => {
    expect(splitList(" a, ,b ,c")).toEqual(["a", "b", "c"]);
  });
  it("explains the kill switch", () => {
    expect(describeVPNState("down", true)).toMatch(/no internet/);
    expect(describeVPNState("direct", false)).toMatch(/WITHOUT the VPN/);
  });
});
