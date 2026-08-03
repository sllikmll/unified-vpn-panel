package service

import (
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"time"

	"github.com/mhsanaei/3x-ui/v3/internal/database"
	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
	"github.com/mhsanaei/3x-ui/v3/internal/util/common"

	"gorm.io/gorm"
)

type GroupSummary struct {
	Name               string `json:"name" example:"customer-a"`
	Description        string `json:"description" example:"Team access profile"`
	Enable             bool   `json:"enable" example:"true"`
	AssignedInboundIds []int  `json:"assignedInboundIds" example:"[1]"`
	DefaultTotalGB     int64  `json:"defaultTotalGB" example:"107374182400"`
	DefaultExpiryTime  int64  `json:"defaultExpiryTime" example:"1893456000000"`
	ClientCount        int    `json:"clientCount" example:"5"`
	TrafficUsed        int64  `json:"trafficUsed" example:"1048576"`
	Up                 int64  `json:"up" example:"524288"`
	Down               int64  `json:"down" example:"524288"`
}

type GroupPolicy struct {
	DefaultTotalGB    int64 `json:"defaultTotalGB" example:"107374182400"`
	DefaultExpiryTime int64 `json:"defaultExpiryTime" example:"1893456000000"`
}

type GroupUpsertRequest struct {
	Name               string      `json:"name" example:"customer-a"`
	Description        string      `json:"description" example:"Team access profile"`
	Enable             bool        `json:"enable" example:"true"`
	AssignedInboundIds []int       `json:"assignedInboundIds" example:"[1]"`
	Policy             GroupPolicy `json:"policy"`
}

type GroupUpdateRequest struct {
	OldName string `json:"oldName" example:"customer-a"`
	GroupUpsertRequest
}

type GroupApplyResult struct {
	Affected int `json:"affected" example:"5"`
	Attached int `json:"attached" example:"10"`
	Detached int `json:"detached" example:"2"`
	Updated  int `json:"updated" example:"5"`
}

func (s *ClientService) ListGroups() ([]GroupSummary, error) {
	db := database.GetDB()
	// email is unique in both clients and client_traffics, so the LEFT JOIN
	// never double-counts a client's traffic.
	type derivedGroupSummary struct {
		Name        string
		ClientCount int
		TrafficUsed int64
		Up          int64
		Down        int64
	}
	var derived []derivedGroupSummary
	if err := db.Table("clients AS c").
		Select("c.group_name AS name, COUNT(*) AS client_count, COALESCE(SUM(ct.up + ct.down), 0) AS traffic_used, COALESCE(SUM(ct.up), 0) AS up, COALESCE(SUM(ct.down), 0) AS down").
		Joins("LEFT JOIN client_traffics ct ON ct.email = c.email").
		Where("c.group_name <> ''").
		Group("c.group_name").
		Scan(&derived).Error; err != nil {
		return nil, err
	}
	var stored []model.ClientGroup
	if err := db.Find(&stored).Error; err != nil {
		return nil, err
	}
	type groupAgg struct {
		count int
		up    int64
		down  int64
		row   *model.ClientGroup
	}
	baseUp := make(map[string]int64, len(stored))
	baseDown := make(map[string]int64, len(stored))
	merged := make(map[string]groupAgg, len(derived)+len(stored))
	for _, g := range stored {
		row := g
		merged[g.Name] = groupAgg{row: &row}
		baseUp[g.Name] = g.ResetUp
		baseDown[g.Name] = g.ResetDown
	}
	for _, g := range derived {
		agg := merged[g.Name]
		agg.count = g.ClientCount
		agg.up = g.Up
		agg.down = g.Down
		merged[g.Name] = agg
	}
	out := make([]GroupSummary, 0, len(merged))
	for name, agg := range merged {
		up := max(agg.up-baseUp[name], 0)
		down := max(agg.down-baseDown[name], 0)
		summary := GroupSummary{Name: name, Enable: true, ClientCount: agg.count, TrafficUsed: up + down, Up: up, Down: down}
		if agg.row != nil {
			summary.Description = agg.row.Description
			summary.Enable = agg.row.Enable
			summary.AssignedInboundIds = parseGroupInboundIDs(agg.row.AssignedInboundIds)
			summary.DefaultTotalGB = agg.row.DefaultTotalGB
			summary.DefaultExpiryTime = agg.row.DefaultExpiryTime
		}
		out = append(out, summary)
	}
	sort.Slice(out, func(i, j int) bool {
		return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name)
	})
	return out, nil
}

// adjustGroupBaselinesForRemovedTraffic shifts group baselines down by the clients'
// current counters so ListGroups totals survive a traffic reset or client delete (#5675).
func adjustGroupBaselinesForRemovedTraffic(tx *gorm.DB, emails []string) error {
	if len(emails) == 0 {
		return nil
	}
	type groupDelta struct {
		Name string
		Up   int64
		Down int64
	}
	totals := make(map[string]*groupDelta)
	for _, batch := range chunkStrings(emails, sqlInChunk) {
		var part []groupDelta
		if err := tx.Table("clients AS c").
			Select("c.group_name AS name, COALESCE(SUM(ct.up), 0) AS up, COALESCE(SUM(ct.down), 0) AS down").
			Joins("JOIN client_traffics ct ON ct.email = c.email").
			Where("c.group_name <> '' AND c.email IN ?", batch).
			Group("c.group_name").
			Scan(&part).Error; err != nil {
			return err
		}
		for i := range part {
			if agg, ok := totals[part[i].Name]; ok {
				agg.Up += part[i].Up
				agg.Down += part[i].Down
			} else {
				totals[part[i].Name] = &part[i]
			}
		}
	}
	for name, d := range totals {
		if d.Up == 0 && d.Down == 0 {
			continue
		}
		res := tx.Model(&model.ClientGroup{}).Where("name = ?", name).Updates(map[string]any{
			"reset_up":   gorm.Expr("reset_up - ?", d.Up),
			"reset_down": gorm.Expr("reset_down - ?", d.Down),
		})
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			if err := tx.Create(&model.ClientGroup{Name: name, Enable: true, AssignedInboundIds: "[]", ResetUp: -d.Up, ResetDown: -d.Down}).Error; err != nil {
				return err
			}
		}
	}
	return nil
}

func (s *ClientService) EmailsByGroup(name string) ([]string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return []string{}, nil
	}
	db := database.GetDB()
	var emails []string
	if err := db.Model(&model.ClientRecord{}).
		Where("group_name = ?", name).
		Order("email ASC").
		Pluck("email", &emails).Error; err != nil {
		return nil, err
	}
	if emails == nil {
		emails = []string{}
	}
	return emails, nil
}

func (s *ClientService) ResetGroupTraffic(name string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return common.NewError("group name is required")
	}
	db := database.GetDB()
	var agg struct {
		Up   int64
		Down int64
	}
	if err := db.Table("clients AS c").
		Select("COALESCE(SUM(ct.up), 0) AS up, COALESCE(SUM(ct.down), 0) AS down").
		Joins("LEFT JOIN client_traffics ct ON ct.email = c.email").
		Where("c.group_name = ?", name).
		Scan(&agg).Error; err != nil {
		return err
	}
	var count int64
	if err := db.Model(&model.ClientGroup{}).Where("name = ?", name).Count(&count).Error; err != nil {
		return err
	}
	if count == 0 {
		return db.Create(&model.ClientGroup{Name: name, Enable: true, AssignedInboundIds: "[]", ResetUp: agg.Up, ResetDown: agg.Down}).Error
	}
	return db.Model(&model.ClientGroup{}).Where("name = ?", name).
		Updates(map[string]any{"reset_up": agg.Up, "reset_down": agg.Down}).Error
}

func (s *ClientService) CreateGroup(name string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return common.NewError("group name is required")
	}
	db := database.GetDB()
	var count int64
	if err := db.Model(&model.ClientGroup{}).Where("name = ?", name).Count(&count).Error; err != nil {
		return err
	}
	if count > 0 {
		return common.NewError("group already exists")
	}
	return db.Create(&model.ClientGroup{Name: name, Enable: true, AssignedInboundIds: "[]"}).Error
}

func (s *ClientService) CreateGroupWithConfig(inboundSvc *InboundService, req GroupUpsertRequest) (GroupApplyResult, error) {
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" {
		return GroupApplyResult{}, common.NewError("group name is required")
	}
	if err := validateGroupPolicy(req.Policy); err != nil {
		return GroupApplyResult{}, err
	}
	if err := validateInboundIDs(req.AssignedInboundIds); err != nil {
		return GroupApplyResult{}, err
	}
	row := model.ClientGroup{
		Name:               req.Name,
		Description:        strings.TrimSpace(req.Description),
		Enable:             req.Enable,
		AssignedInboundIds: marshalGroupInboundIDs(req.AssignedInboundIds),
		DefaultTotalGB:     req.Policy.DefaultTotalGB,
		DefaultExpiryTime:  req.Policy.DefaultExpiryTime,
	}
	if err := database.GetDB().Create(&row).Error; err != nil {
		return GroupApplyResult{}, err
	}
	return s.ApplyGroupAssignments(inboundSvc, req.Name)
}

func (s *ClientService) UpdateGroupWithConfig(inboundSvc *InboundService, req GroupUpdateRequest) (GroupApplyResult, error) {
	oldName := strings.TrimSpace(req.OldName)
	req.Name = strings.TrimSpace(req.Name)
	if oldName == "" {
		return GroupApplyResult{}, common.NewError("old group name is required")
	}
	if req.Name == "" {
		return GroupApplyResult{}, common.NewError("new group name is required")
	}
	if err := validateGroupPolicy(req.Policy); err != nil {
		return GroupApplyResult{}, err
	}
	if err := validateInboundIDs(req.AssignedInboundIds); err != nil {
		return GroupApplyResult{}, err
	}
	affected := 0
	var renamedIDs []int
	if oldName != req.Name {
		var idErr error
		renamedIDs, idErr = s.recordIDsByGroup(oldName)
		if idErr != nil {
			return GroupApplyResult{}, idErr
		}
		var err error
		affected, err = s.RenameGroup(oldName, req.Name)
		if err != nil {
			return GroupApplyResult{}, err
		}
	}
	updates := map[string]any{
		"description":          strings.TrimSpace(req.Description),
		"enable":               req.Enable,
		"assigned_inbound_ids": marshalGroupInboundIDs(req.AssignedInboundIds),
		"default_total_gb":     req.Policy.DefaultTotalGB,
		"default_expiry_time":  req.Policy.DefaultExpiryTime,
	}
	res := database.GetDB().Model(&model.ClientGroup{}).Where("name = ?", req.Name).Updates(updates)
	if res.Error != nil {
		return GroupApplyResult{}, res.Error
	}
	if res.RowsAffected == 0 {
		row := model.ClientGroup{
			Name:               req.Name,
			Description:        strings.TrimSpace(req.Description),
			Enable:             req.Enable,
			AssignedInboundIds: marshalGroupInboundIDs(req.AssignedInboundIds),
			DefaultTotalGB:     req.Policy.DefaultTotalGB,
			DefaultExpiryTime:  req.Policy.DefaultExpiryTime,
		}
		if err := database.GetDB().Create(&row).Error; err != nil {
			return GroupApplyResult{}, err
		}
	}
	result, err := s.ApplyGroupAssignments(inboundSvc, req.Name)
	result.Affected += affected
	touched, touchErr := s.touchRecords(inboundSvc, renamedIDs)
	result.Updated += touched
	if touchErr != nil {
		return result, touchErr
	}
	return result, err
}

func (s *ClientService) RenameGroup(oldName, newName string) (int, error) {
	oldName = strings.TrimSpace(oldName)
	newName = strings.TrimSpace(newName)
	if oldName == "" {
		return 0, common.NewError("old group name is required")
	}
	if newName == "" {
		return 0, common.NewError("new group name is required")
	}
	if oldName == newName {
		return 0, nil
	}
	return s.replaceGroupValue(oldName, newName)
}

func (s *ClientService) RenameGroupAndApply(inboundSvc *InboundService, oldName, newName string) (GroupApplyResult, error) {
	ids, err := s.recordIDsByGroup(strings.TrimSpace(oldName))
	if err != nil {
		return GroupApplyResult{}, err
	}
	affected, err := s.RenameGroup(oldName, newName)
	if err != nil {
		return GroupApplyResult{}, err
	}
	result := GroupApplyResult{Affected: affected}
	touched, err := s.touchRecords(inboundSvc, ids)
	result.Updated = touched
	return result, err
}

func (s *ClientService) DeleteGroup(name string) (int, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return 0, common.NewError("group name is required")
	}
	return s.replaceGroupValue(name, "")
}

func (s *ClientService) DeleteGroupAndApply(inboundSvc *InboundService, name string) (GroupApplyResult, error) {
	name = strings.TrimSpace(name)
	ids, err := s.recordIDsByGroup(name)
	if err != nil {
		return GroupApplyResult{}, err
	}
	result, err := s.applyGroupAssignmentsToRecords(inboundSvc, name, ids, true)
	if err != nil {
		return result, err
	}
	affected, err := s.DeleteGroup(name)
	if err != nil {
		return result, err
	}
	result.Affected = affected
	touched, err := s.touchRecords(inboundSvc, ids)
	result.Updated += touched
	return result, err
}

func (s *ClientService) RemoveFromGroup(emails []string) (int, error) {
	return s.AddToGroup(emails, "")
}

func (s *ClientService) AddToGroupAndApply(inboundSvc *InboundService, emails []string, group string) (GroupApplyResult, error) {
	records, err := s.recordsForEmails(emails)
	if err != nil {
		return GroupApplyResult{}, err
	}
	affected, err := s.AddToGroup(emails, group)
	if err != nil {
		return GroupApplyResult{}, err
	}
	result, err := s.ApplyGroupAssignments(inboundSvc, group)
	result.Affected = affected
	touched, touchErr := s.touchRecords(inboundSvc, recordIDs(records))
	result.Updated += touched
	if touchErr != nil {
		return result, touchErr
	}
	return result, err
}

func (s *ClientService) RemoveFromGroupAndApply(inboundSvc *InboundService, emails []string) (GroupApplyResult, error) {
	records, err := s.recordsForEmails(emails)
	if err != nil {
		return GroupApplyResult{}, err
	}
	byGroup := map[string][]int{}
	for i := range records {
		if records[i].Group != "" {
			byGroup[records[i].Group] = append(byGroup[records[i].Group], records[i].Id)
		}
	}
	affected, err := s.RemoveFromGroup(emails)
	if err != nil {
		return GroupApplyResult{}, err
	}
	result := GroupApplyResult{Affected: affected}
	for group, ids := range byGroup {
		part, applyErr := s.applyGroupAssignmentsToRecords(inboundSvc, group, ids, true)
		result.Attached += part.Attached
		result.Detached += part.Detached
		result.Updated += part.Updated
		if applyErr != nil {
			return result, applyErr
		}
	}
	touched, err := s.touchRecords(inboundSvc, recordIDs(records))
	result.Updated += touched
	if err != nil {
		return result, err
	}
	return result, nil
}

func (s *ClientService) AddToGroup(emails []string, group string) (int, error) {
	group = strings.TrimSpace(group)
	if len(emails) == 0 {
		return 0, nil
	}
	db := database.GetDB()

	if group != "" {
		var exists int64
		if err := db.Model(&model.ClientGroup{}).Where("name = ?", group).Count(&exists).Error; err != nil {
			return 0, err
		}
		if exists == 0 {
			var derived int64
			if err := db.Model(&model.ClientRecord{}).Where("group_name = ?", group).Count(&derived).Error; err != nil {
				return 0, err
			}
			if derived == 0 {
				if err := db.Create(&model.ClientGroup{Name: group, Enable: true, AssignedInboundIds: "[]"}).Error; err != nil {
					return 0, err
				}
			}
		}
	}

	var records []model.ClientRecord
	for _, batch := range chunkStrings(emails, sqlInChunk) {
		var rows []model.ClientRecord
		if err := db.Where("email IN ?", batch).Find(&rows).Error; err != nil {
			return 0, err
		}
		records = append(records, rows...)
	}
	if len(records) == 0 {
		return 0, nil
	}
	affectedEmails := make([]string, 0, len(records))
	for _, r := range records {
		affectedEmails = append(affectedEmails, r.Email)
	}

	tx := db.Begin()
	for _, batch := range chunkStrings(affectedEmails, sqlInChunk) {
		if err := tx.Model(&model.ClientRecord{}).
			Where("email IN ?", batch).
			UpdateColumn("group_name", group).Error; err != nil {
			tx.Rollback()
			return 0, err
		}
	}

	var inboundIDs []int
	inboundIDSeen := make(map[int]struct{})
	for _, batch := range chunkStrings(affectedEmails, sqlInChunk) {
		var ids []int
		if err := tx.Table("client_inbounds").
			Joins("JOIN clients ON clients.id = client_inbounds.client_id").
			Where("clients.email IN ?", batch).
			Distinct("client_inbounds.inbound_id").
			Pluck("inbound_id", &ids).Error; err != nil {
			tx.Rollback()
			return 0, err
		}
		for _, id := range ids {
			if _, ok := inboundIDSeen[id]; !ok {
				inboundIDSeen[id] = struct{}{}
				inboundIDs = append(inboundIDs, id)
			}
		}
	}

	emailSet := make(map[string]struct{}, len(affectedEmails))
	for _, e := range affectedEmails {
		emailSet[e] = struct{}{}
	}

	for _, ibID := range inboundIDs {
		var ib model.Inbound
		if err := tx.First(&ib, ibID).Error; err != nil {
			tx.Rollback()
			return 0, err
		}
		var settings map[string]any
		if err := json.Unmarshal([]byte(ib.Settings), &settings); err != nil {
			continue
		}
		clients, ok := settings["clients"].([]any)
		if !ok {
			continue
		}
		modified := false
		for i := range clients {
			cm, ok := clients[i].(map[string]any)
			if !ok {
				continue
			}
			email, _ := cm["email"].(string)
			if _, hit := emailSet[email]; !hit {
				continue
			}
			if group == "" {
				delete(cm, "group")
			} else {
				cm["group"] = group
			}
			clients[i] = cm
			modified = true
		}
		if modified {
			settings["clients"] = clients
			newSettings, err := json.Marshal(settings)
			if err != nil {
				continue
			}
			ib.Settings = string(newSettings)
			if err := tx.Save(&ib).Error; err != nil {
				tx.Rollback()
				return 0, err
			}
		}
	}

	if err := tx.Commit().Error; err != nil {
		return 0, err
	}
	return len(records), nil
}

func (s *ClientService) ApplyGroupAssignments(inboundSvc *InboundService, name string) (GroupApplyResult, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return GroupApplyResult{}, common.NewError("group name is required")
	}
	var ids []int
	if err := database.GetDB().Model(&model.ClientRecord{}).Where("group_name = ?", name).Pluck("id", &ids).Error; err != nil {
		return GroupApplyResult{}, err
	}
	return s.applyGroupAssignmentsToRecords(inboundSvc, name, ids, false)
}

func (s *ClientService) applyGroupAssignmentsToRecords(inboundSvc *InboundService, name string, recordIDs []int, forceDetach bool) (GroupApplyResult, error) {
	group, err := s.getGroupRow(name)
	if err != nil {
		return GroupApplyResult{}, err
	}
	configuredIDs := parseGroupInboundIDs(group.AssignedInboundIds)
	configured := map[int]struct{}{}
	for _, id := range configuredIDs {
		configured[id] = struct{}{}
	}
	target := map[int]struct{}{}
	if !forceDetach && group.Enable {
		for _, id := range configuredIDs {
			target[id] = struct{}{}
		}
	}
	result := GroupApplyResult{}
	for _, id := range recordIDs {
		rec, err := s.GetByID(id)
		if err != nil {
			return result, err
		}
		current, err := s.GetInboundIdsForRecord(id)
		if err != nil {
			return result, err
		}
		have := map[int]struct{}{}
		for _, ibID := range current {
			have[ibID] = struct{}{}
		}
		var attachIDs []int
		var detachIDs []int
		for ibID := range target {
			if _, ok := have[ibID]; !ok {
				attachIDs = append(attachIDs, ibID)
			}
		}
		for _, ibID := range current {
			_, inTarget := target[ibID]
			_, inConfigured := configured[ibID]
			if len(configured) > 0 && !inTarget && inConfigured {
				detachIDs = append(detachIDs, ibID)
			}
		}
		if len(attachIDs) > 0 {
			if _, err := s.Attach(inboundSvc, id, attachIDs); err != nil {
				return result, err
			}
			result.Attached += len(attachIDs)
		}
		if len(detachIDs) > 0 {
			if _, err := s.Detach(inboundSvc, id, detachIDs); err != nil {
				return result, err
			}
			result.Detached += len(detachIDs)
		}
		updated := rec.ToClient()
		changed := applyGroupPolicyDefaults(updated, group)
		if group.Enable != rec.Enable {
			updated.Enable = group.Enable
			changed = true
		}
		if changed && !forceDetach {
			if _, err := s.Update(inboundSvc, id, *updated); err != nil {
				return result, err
			}
			result.Updated++
		}
	}
	return result, nil
}

func (s *ClientService) recordsForEmails(emails []string) ([]model.ClientRecord, error) {
	if len(emails) == 0 {
		return nil, nil
	}
	var records []model.ClientRecord
	for _, batch := range chunkStrings(emails, sqlInChunk) {
		var rows []model.ClientRecord
		if err := database.GetDB().Where("email IN ?", batch).Find(&rows).Error; err != nil {
			return nil, err
		}
		records = append(records, rows...)
	}
	return records, nil
}

func (s *ClientService) recordIDsByGroup(name string) ([]int, error) {
	var ids []int
	if err := database.GetDB().Model(&model.ClientRecord{}).Where("group_name = ?", name).Pluck("id", &ids).Error; err != nil {
		return nil, err
	}
	return ids, nil
}

func recordIDs(records []model.ClientRecord) []int {
	ids := make([]int, 0, len(records))
	for i := range records {
		ids = append(ids, records[i].Id)
	}
	return ids
}

func (s *ClientService) touchRecords(inboundSvc *InboundService, ids []int) (int, error) {
	updated := 0
	for _, id := range ids {
		rec, err := s.GetByID(id)
		if err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				continue
			}
			return updated, err
		}
		if _, err := s.Update(inboundSvc, id, *rec.ToClient()); err != nil {
			return updated, err
		}
		updated++
	}
	return updated, nil
}

func (s *ClientService) getGroupRow(name string) (*model.ClientGroup, error) {
	var group model.ClientGroup
	err := database.GetDB().Where("name = ?", name).First(&group).Error
	if err == nil {
		return &group, nil
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return &model.ClientGroup{Name: name, Enable: true, AssignedInboundIds: "[]"}, nil
	}
	return nil, err
}

func applyGroupPolicyDefaults(c *model.Client, group *model.ClientGroup) bool {
	changed := false
	if group.DefaultTotalGB > 0 && c.TotalGB == 0 {
		c.TotalGB = group.DefaultTotalGB
		changed = true
	}
	if group.DefaultExpiryTime > 0 && c.ExpiryTime == 0 {
		c.ExpiryTime = group.DefaultExpiryTime
		changed = true
	}
	return changed
}

func validateGroupPolicy(policy GroupPolicy) error {
	if policy.DefaultTotalGB < 0 {
		return common.NewError("default traffic limit must not be negative")
	}
	if policy.DefaultExpiryTime < 0 {
		return common.NewError("default expiry time must not be negative")
	}
	if policy.DefaultExpiryTime > 0 && policy.DefaultExpiryTime < time.Now().UnixMilli() {
		return common.NewError("default expiry time must be in the future")
	}
	return nil
}

func validateInboundIDs(ids []int) error {
	if len(ids) == 0 {
		return nil
	}
	seen := map[int]struct{}{}
	for _, id := range ids {
		if id <= 0 {
			return common.NewError("inbound id must be positive")
		}
		seen[id] = struct{}{}
	}
	var count int64
	keys := make([]int, 0, len(seen))
	for id := range seen {
		keys = append(keys, id)
	}
	if err := database.GetDB().Model(&model.Inbound{}).Where("id IN ?", keys).Count(&count).Error; err != nil {
		return err
	}
	if count != int64(len(keys)) {
		return common.NewError("assigned inbound not found")
	}
	return nil
}

func parseGroupInboundIDs(raw string) []int {
	var ids []int
	if err := json.Unmarshal([]byte(strings.TrimSpace(raw)), &ids); err != nil {
		return []int{}
	}
	out := make([]int, 0, len(ids))
	seen := map[int]struct{}{}
	for _, id := range ids {
		if id <= 0 {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	sort.Ints(out)
	return out
}

func marshalGroupInboundIDs(ids []int) string {
	clean := parseGroupInboundIDs(mustJSON(ids))
	b, err := json.Marshal(clean)
	if err != nil {
		return "[]"
	}
	return string(b)
}

func mustJSON(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return "[]"
	}
	return string(b)
}

func (s *ClientService) replaceGroupValue(oldName, newName string) (int, error) {
	db := database.GetDB()
	if newName == "" {
		if err := db.Where("name = ?", oldName).Delete(&model.ClientGroup{}).Error; err != nil {
			return 0, err
		}
	} else {
		if err := db.Model(&model.ClientGroup{}).Where("name = ?", oldName).Update("name", newName).Error; err != nil {
			return 0, err
		}
	}
	var records []model.ClientRecord
	if err := db.Where("group_name = ?", oldName).Find(&records).Error; err != nil {
		return 0, err
	}
	if len(records) == 0 {
		return 0, nil
	}
	affectedEmails := make([]string, 0, len(records))
	for _, r := range records {
		affectedEmails = append(affectedEmails, r.Email)
	}

	tx := db.Begin()
	if err := tx.Model(&model.ClientRecord{}).
		Where("group_name = ?", oldName).
		UpdateColumn("group_name", newName).Error; err != nil {
		tx.Rollback()
		return 0, err
	}

	var inboundIDs []int
	inboundIDSeen := make(map[int]struct{})
	for _, batch := range chunkStrings(affectedEmails, sqlInChunk) {
		var ids []int
		if err := tx.Table("client_inbounds").
			Joins("JOIN clients ON clients.id = client_inbounds.client_id").
			Where("clients.email IN ?", batch).
			Distinct("client_inbounds.inbound_id").
			Pluck("inbound_id", &ids).Error; err != nil {
			tx.Rollback()
			return 0, err
		}
		for _, id := range ids {
			if _, ok := inboundIDSeen[id]; !ok {
				inboundIDSeen[id] = struct{}{}
				inboundIDs = append(inboundIDs, id)
			}
		}
	}

	for _, ibID := range inboundIDs {
		var ib model.Inbound
		if err := tx.First(&ib, ibID).Error; err != nil {
			tx.Rollback()
			return 0, err
		}
		var settings map[string]any
		if err := json.Unmarshal([]byte(ib.Settings), &settings); err != nil {
			continue
		}
		clients, ok := settings["clients"].([]any)
		if !ok {
			continue
		}
		modified := false
		for i := range clients {
			cm, ok := clients[i].(map[string]any)
			if !ok {
				continue
			}
			if g, ok := cm["group"].(string); ok && g == oldName {
				if newName == "" {
					delete(cm, "group")
				} else {
					cm["group"] = newName
				}
				clients[i] = cm
				modified = true
			}
		}
		if modified {
			settings["clients"] = clients
			newSettings, err := json.Marshal(settings)
			if err != nil {
				continue
			}
			ib.Settings = string(newSettings)
			if err := tx.Save(&ib).Error; err != nil {
				tx.Rollback()
				return 0, err
			}
		}
	}

	if err := tx.Commit().Error; err != nil {
		return 0, err
	}
	return len(records), nil
}
