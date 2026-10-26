package config

// This file checks Wi-Fi country codes: the two-letter codes the Linux
// regulatory database knows, with a hint for common mistakes (UK for GB).

import "strings"

// countryCodes are the ISO 3166-1 two-letter country codes. The Wi-Fi
// rules (which channels and how much power) are set per country, and the
// radio only knows countries by these codes: "UK", for example, isn't one;
// the United Kingdom is "GB".
var countryCodes = strings.Fields(`
AD AE AF AG AI AL AM AO AQ AR AS AT AU AW AX AZ BA BB BD BE BF BG BH BI BJ BL
BM BN BO BQ BR BS BT BV BW BY BZ CA CC CD CF CG CH CI CK CL CM CN CO CR CU CV
CW CX CY CZ DE DJ DK DM DO DZ EC EE EG EH ER ES ET FI FJ FK FM FO FR GA GB GD
GE GF GG GH GI GL GM GN GP GQ GR GS GT GU GW GY HK HM HN HR HT HU ID IE IL IM
IN IO IQ IR IS IT JE JM JO JP KE KG KH KI KM KN KP KR KW KY KZ LA LB LC LI LK
LR LS LT LU LV LY MA MC MD ME MF MG MH MK ML MM MN MO MP MQ MR MS MT MU MV MW
MX MY MZ NA NC NE NF NG NI NL NO NP NR NU NZ OM PA PE PF PG PH PK PL PM PN PR
PS PT PW PY QA RE RO RS RU RW SA SB SC SD SE SG SH SI SJ SK SL SM SN SO SR SS
ST SV SX SY SZ TC TD TF TG TH TJ TK TL TM TN TO TR TT TV TW TZ UA UG UM US UY
UZ VA VC VE VG VI VN VU WF WS YE YT ZA ZM ZW
`)

// isCountryCode reports whether code is an ISO 3166-1 two-letter code, in
// capitals.
func isCountryCode(code string) bool {
	for _, known := range countryCodes {
		if code == known {
			return true
		}
	}
	return false
}

// countryMessage explains why code isn't a country code, with a hint for
// the common mistake of writing "UK".
func countryMessage(code string) string {
	if strings.EqualFold(code, "UK") {
		return "Use GB for the United Kingdom."
	}
	return "Must be a two-letter country code such as US."
}
