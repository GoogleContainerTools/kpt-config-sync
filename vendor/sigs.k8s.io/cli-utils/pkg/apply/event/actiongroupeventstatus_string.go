// Code generated by "stringer -type=ActionGroupEventStatus"; DO NOT EDIT.

package event

import "strconv"

func _() {
	// An "invalid array index" compiler error signifies that the constant values have changed.
	// Re-run the stringer command to generate them again.
	var x [1]struct{}
	_ = x[Started-0]
	_ = x[Finished-1]
}

const _ActionGroupEventStatus_name = "StartedFinished"

var _ActionGroupEventStatus_index = [...]uint8{0, 7, 15}

func (i ActionGroupEventStatus) String() string {
	if i < 0 || i >= ActionGroupEventStatus(len(_ActionGroupEventStatus_index)-1) {
		return "ActionGroupEventStatus(" + strconv.FormatInt(int64(i), 10) + ")"
	}
	return _ActionGroupEventStatus_name[_ActionGroupEventStatus_index[i]:_ActionGroupEventStatus_index[i+1]]
}
