package handler

import "strings"

// Teaching choices describe recommendations, never remote activation evidence.
type classroomTeachingPlan struct {
	Version   string   `json:"version"`
	Skills    []string `json:"recommended_skills"`
	Ability   string   `json:"ability"`
	Narration bool     `json:"narration_required"`
	Language  string   `json:"language"`
	Pacing    string   `json:"pacing"`
}

func recommendClassroomTeaching(requirement string, preferences []string) classroomTeachingPlan {
	plan := classroomTeachingPlan{Version: "teaching-v1", Skills: []string{"feynman-learning"}, Ability: "not_assessed", Narration: true, Language: "zh-CN", Pacing: "normal"}
	// Course goals select pedagogy. Personal preferences cannot select ability.
	for _, goal := range []string{"操作练习", "动手练习", "实操训练", "工作坊"} {
		if strings.Contains(requirement, goal) {
			plan.Skills = []string{"workshop-style"}
			break
		}
	}
	for _, preference := range preferences {
		if strings.Contains(preference, "慢速") || strings.Contains(preference, "放慢") {
			plan.Pacing = "slow"
		}
	}
	return plan
}
