// Code generated by "stringer -type=Policy -linecomment"; DO NOT EDIT.

package inventory

import "strconv"

func _() {
	// An "invalid array index" compiler error signifies that the constant values have changed.
	// Re-run the stringer command to generate them again.
	var x [1]struct{}
	_ = x[PolicyMustMatch-0]
	_ = x[PolicyAdoptIfNoInventory-1]
	_ = x[PolicyAdoptAll-2]
}

const _Policy_name = "MustMatchAdoptIfNoInventoryAdoptAll"

var _Policy_index = [...]uint8{0, 9, 27, 35}

func (i Policy) String() string {
	if i < 0 || i >= Policy(len(_Policy_index)-1) {
		return "Policy(" + strconv.FormatInt(int64(i), 10) + ")"
	}
	return _Policy_name[_Policy_index[i]:_Policy_index[i+1]]
}
