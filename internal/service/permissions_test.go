package service

import "testing"

func TestCanEdit(t *testing.T) {
	id := int64(7)
	cases := []struct {
		name, role     string
		actor, creator int64
		assignee       *int64
		fields         []string
		want           bool
	}{
		{"owner any task", "owner", 1, 2, nil, []string{"title"}, true},
		{"admin any task", "admin", 1, 2, nil, []string{"assignee_id"}, true},
		{"creator changes assignee", "member", 2, 2, nil, []string{"assignee_id"}, true},
		{"assignee changes status", "member", 7, 2, &id, []string{"status"}, true},
		{"assignee cannot reassign", "member", 7, 2, &id, []string{"assignee_id"}, false},
		{"assignee cannot edit title", "member", 7, 2, &id, []string{"title"}, false},
		{"other member cannot edit", "member", 9, 2, &id, []string{"status"}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := CanEdit(tc.role, tc.actor, tc.creator, tc.assignee, tc.fields)
			if got != tc.want {
				t.Errorf("got %v want %v", got, tc.want)
			}
		})
	}
}
