package collector

// Ports share one fetch/action/pagination path; each extracts independent rows
// from the fetched document. The primary port determines page completion.
type OutputPort struct {
	Records Locator `json:"records"`
	Output  Output  `json:"output"`
}

func (d Definition) Ports() []OutputPort {
	return append([]OutputPort{{Records: d.Records, Output: d.Output}}, d.AdditionalOutputs...)
}
func (d Definition) ForPort(name string) (Definition, error) {
	for _, p := range d.Ports() {
		if p.Output.Port == name {
			d.Records, d.Output, d.AdditionalOutputs = p.Records, p.Output, nil
			return d, nil
		}
	}
	return Definition{}, Invalid("port", "采集器未声明此输出端口："+name)
}
func PortSchema(o Output) any {
	return struct {
		Port           string   `json:"port"`
		Fields         []Field  `json:"fields"`
		IdentityFields []string `json:"identity_fields"`
	}{o.Port, o.Fields, o.IdentityFields}
}
func CandidatePort(c Candidate) string {
	if c.Port == "" {
		return "records"
	}
	return c.Port
}
func CandidateKey(c Candidate) string { return CandidatePort(c) + ":" + c.Identity }
func SampleValue(c Candidate) map[string]any {
	if c.Port == "" {
		return c.Values
	}
	return map[string]any{"port": c.Port, "values": c.Values}
}
