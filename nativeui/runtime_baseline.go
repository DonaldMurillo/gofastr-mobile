package nativeui

// actionScreen supplies the editor's durable baseline rather than a newly read
// repository form, including after a draft is restored over newer saved data.
func (runtime *Runtime) actionScreen(session *runtimeSession) Screen {
	screen := session.screen
	if screen.Form != nil && session.editor != nil {
		form := *screen.Form
		form.Fields = append([]Field(nil), form.Fields...)
		baseline := session.editor.Snapshot().Baseline
		if session.restoreBaseline != nil {
			baseline = session.restoreBaseline
		}
		for index := range form.Fields {
			form.Fields[index].Value = baseline[form.Fields[index].Key]
		}
		screen.Form = &form
	}
	return screen
}
