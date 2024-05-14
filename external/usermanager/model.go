package usermanager

type UsageInfo struct {
	Allowance      string `json:"allowance"`
	Used           string `json:"used"`
	UsedPercentage string `json:"used_percentage"`
}
