package co

import (
	"github.com/nekoimi/scrapio/internal/collector"
	"sort"
	"xorm.io/xorm"
)

type PortSave struct {
	Port string      `json:"port"`
	Save SaveBinding `json:"save"`
}

func (st TaskStage) Saves() []PortSave {
	return append([]PortSave{{Port: "records", Save: st.Save}}, st.AdditionalSaves...)
}
func (st TaskStage) Binding(port string) (SaveBinding, error) {
	for _, p := range st.Saves() {
		if p.Port == port {
			return p.Save, nil
		}
	}
	return SaveBinding{}, collector.Invalid("save."+port, "每个命名端口需明确保存或仅传递策略")
}
func passThrough(b SaveBinding) bool { return b.Mode == "pass_through" }
func validateSaveMode(b SaveBinding) error {
	if b.Mode != "" && b.Mode != "table" && b.Mode != "pass_through" {
		return collector.Invalid("save.mode", "需table或pass_through")
	}
	if passThrough(b) && (b.TableID != "" || b.SchemaVersion != 0 || b.SchemaHash != "" || len(b.Mapping) > 0 || len(b.UniqueKey) > 0 || b.UpdatePolicy != "" || b.NullPolicy != "") {
		return collector.Invalid("save", "仅传递模式不能携带数据表绑定；输出仍持久化，不写业务表")
	}
	return nil
}

func validateOutputCandidate(d collector.Definition, c collector.Candidate) error {
	if c.Valid && c.Identity == "" {
		return collector.Invalid("output.identity", "有效输出必须包含稳定身份")
	}
	fields := map[string]collector.Field{}
	for _, f := range d.Output.Fields {
		fields[f.Key] = f
		v, present := c.Values[f.Key]
		if !present {
			if f.Required {
				return collector.Invalid("output."+f.Key, "缺少必填输出字段")
			}
			continue
		}
		if _, e := collector.ValidateValue(v, f, false); e != nil {
			return e
		}
	}
	for k := range c.Values {
		if _, ok := fields[k]; !ok {
			return collector.Invalid("output."+k, "输出包含未声明字段")
		}
	}
	for _, key := range d.Output.IdentityFields {
		v, ok := c.Values[key]
		if !ok || v == nil || v == "" {
			return collector.Invalid("output."+key, "身份字段必须非空")
		}
	}
	return nil
}

// Lock all tables in ID order, then validate each declared port. Called at
// publication, run creation and every page commit, including zero-output pages.
func stageSchemas(s *xorm.Session, owner int64, st TaskStage, d collector.Definition) (map[string]TableSchema, error) {
	if len(st.AdditionalSaves) != len(d.AdditionalOutputs) {
		return nil, collector.Invalid("additional_saves", "附加端口必须逐一配置保存或仅传递策略")
	}
	saves := st.Saves()
	sort.Slice(saves, func(i, j int) bool { return saves[i].Save.TableID < saves[j].Save.TableID })
	result := map[string]TableSchema{}
	for _, p := range saves {
		if _, ok := result[p.Port]; ok {
			return nil, collector.Invalid("additional_saves", "重复端口绑定")
		}
		cd, e := d.ForPort(p.Port)
		if e != nil {
			return nil, e
		}
		if e = validateSaveMode(p.Save); e != nil {
			return nil, e
		}
		schema := TableSchema{Fields: cd.Output.Fields, UniqueKey: cd.Output.IdentityFields}
		if !passThrough(p.Save) {
			schema, e = tableSchema(s, owner, p.Save, true)
			if e != nil {
				return nil, e
			}
			if e = validateBinding(p.Save, schema, cd); e != nil {
				return nil, e
			}
		}
		result[p.Port] = schema
	}
	return result, nil
}
