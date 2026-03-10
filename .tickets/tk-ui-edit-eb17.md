---
id: tk-ui-edit-eb17
stage: done
status: open
risk: low
deps: []
links: []
created: 2026-03-05T05:33:50Z
type: bug
priority: 1
---
# 'tk ui' in edit/create mode (s)ave should save ticket

In `tk ui` edit/create form, there is no dedicated save keybinding. The only way to submit is `enter`, which on choice fields (Type, Priority, Stage) cycles the value instead of submitting. Add `ctrl+s` as a universal save shortcut that submits the form regardless of focused field.

## Review Log

**2026-03-10T05:46:33Z [agent:code-review]**
APPROVED — Clean, minimal change. ctrl+s dispatched before field-specific handlers. Help text updated. Test covers the key scenario (submit from choice field where enter would cycle).
