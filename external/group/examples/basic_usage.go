package main

import (
	"fmt"
	"log"

	group "github.com/ooaklee/ghatd/external/group"
)

// Example 1: Creating a Simple Team
func CreateSimpleTeam() {
	// Set up dependencies
	config := group.DefaultGroupConfig()
	idGen := group.NewDefaultIDGenerator()
	timeProvider := group.NewDefaultTimeProvider()
	stringUtils := group.NewDefaultStringUtils()

	// Create a new team
	team := group.NewUniversalGroup(config, idGen, timeProvider, stringUtils)
	team.Name = "Frontend Engineering Team"
	team.Type = group.GroupTypeTeam
	team.DisplayInfo.Description = "Responsible for all user-facing web applications"
	team.DisplayInfo.Email = "frontend@company.com"
	team.DisplayInfo.Icon = "🎨"

	// Set initial state (generates IDs, timestamps, default status)
	team.SetInitialState()

	// Add members
	team.AddMember("user-alice", group.MemberTypeUser, group.MemberRoleHead)
	team.AddMember("user-bob", group.MemberTypeUser, group.MemberRoleAdmin)
	team.AddMember("user-charlie", group.MemberTypeUser, group.MemberRoleMember)
	team.AddMember("user-diana", group.MemberTypeUser, group.MemberRoleMember)

	// Set leadership
	team.Leadership.HeadID = "user-alice"
	team.Leadership.AdminIDs = []string{"user-bob"}

	// Add custom extensions
	team.SetExtension("github_team", "frontend-eng")
	team.SetExtension("jira_project", "FE")
	team.SetExtension("cost_center", "ENG-001")

	// Validate
	if err := team.Validate(); err != nil {
		log.Fatalf("Validation failed: %v", err)
	}

	fmt.Printf("Created team: %s (ID: %s)\n", team.Name, team.ID)
	fmt.Printf("Members: %d users\n", len(team.GetUserMemberIDs()))
	fmt.Printf("Status: %s\n", team.Status)
}

// Example 2: Creating a Hierarchical Organization
func CreateHierarchicalOrganization() {
	config := group.DefaultGroupConfig()
	idGen := group.NewDefaultIDGenerator()
	timeProvider := group.NewDefaultTimeProvider()
	stringUtils := group.NewDefaultStringUtils()

	// Create company (top level)
	company := group.NewUniversalGroup(config, idGen, timeProvider, stringUtils)
	company.Name = "Acme Corporation"
	company.Type = group.GroupTypeCompany
	company.DisplayInfo.Description = "A leading technology company"
	company.DisplayInfo.Website = "https://acme.com"
	company.SetInitialState()

	// Create engineering department
	engineering := group.NewUniversalGroup(config, idGen, timeProvider, stringUtils)
	engineering.Name = "Engineering"
	engineering.Type = group.GroupTypeDepartment
	engineering.DisplayInfo.Description = "Engineering department"
	engineering.SetInitialState()

	// Create teams under engineering
	frontendTeam := group.NewUniversalGroup(config, idGen, timeProvider, stringUtils)
	frontendTeam.Name = "Frontend Team"
	frontendTeam.Type = group.GroupTypeTeam
	frontendTeam.SetInitialState()

	backendTeam := group.NewUniversalGroup(config, idGen, timeProvider, stringUtils)
	backendTeam.Name = "Backend Team"
	backendTeam.Type = group.GroupTypeTeam
	backendTeam.SetInitialState()

	// Add users to teams
	frontendTeam.AddMember("user-1", group.MemberTypeUser, group.MemberRoleMember)
	frontendTeam.AddMember("user-2", group.MemberTypeUser, group.MemberRoleMember)
	backendTeam.AddMember("user-3", group.MemberTypeUser, group.MemberRoleMember)
	backendTeam.AddMember("user-4", group.MemberTypeUser, group.MemberRoleMember)

	// Build hierarchy: Company -> Engineering -> Teams
	company.AddMember(engineering.ID, group.MemberTypeGroup, group.MemberRoleMember)
	engineering.AddMember(frontendTeam.ID, group.MemberTypeGroup, group.MemberRoleMember)
	engineering.AddMember(backendTeam.ID, group.MemberTypeGroup, group.MemberRoleMember)

	fmt.Printf("Created organization hierarchy:\n")
	fmt.Printf("  %s\n", company.Name)
	fmt.Printf("    └─ %s\n", engineering.Name)
	fmt.Printf("        ├─ %s (%d members)\n", frontendTeam.Name, frontendTeam.GetMemberCount())
	fmt.Printf("        └─ %s (%d members)\n", backendTeam.Name, backendTeam.GetMemberCount())
}

// Example 3: Managing Group Status
func ManageGroupStatus() {
	config := group.DefaultGroupConfig()
	idGen := group.NewDefaultIDGenerator()
	timeProvider := group.NewDefaultTimeProvider()
	stringUtils := group.NewDefaultStringUtils()

	uniGroup := group.NewUniversalGroup(config, idGen, timeProvider, stringUtils)
	uniGroup.Name = "Project Alpha"
	uniGroup.Type = group.GroupTypeProject
	uniGroup.SetInitialState()

	fmt.Printf("Initial status: %s\n", uniGroup.Status)

	// Update status to inactive
	if _, err := uniGroup.UpdateStatus(group.GroupStatusInactive); err != nil {
		log.Printf("Failed to set inactive: %v", err)
	} else {
		fmt.Printf("Status after inactive: %s\n", uniGroup.Status)
	}

	// Archive the group
	if _, err := uniGroup.UpdateStatus(group.GroupStatusArchived); err != nil {
		log.Printf("Failed to archive: %v", err)
	} else {
		fmt.Printf("Status after archive: %s\n", uniGroup.Status)
		fmt.Printf("Archived at: %s\n", uniGroup.Metadata.ArchivedAt)
	}

	// Try invalid transition (should fail)
	if _, err := uniGroup.UpdateStatus(group.GroupStatusProvisioned); err != nil {
		fmt.Printf("Invalid transition blocked: %v\n", err)
	}
}

// Example 4: Custom Group Type (Family Group)
func CreateFamilyGroup() {
	// Custom config for family groups
	config := group.NewCustomGroupConfig().
		WithDefaultStatus(group.GroupStatusActive).
		WithValidTypes(group.GroupTypeFamily).
		WithValidMemberTypes(group.MemberTypeUser). // Only users, no nested groups
		WithNestedGroups(false).
		WithRequiredFields("name")

	idGen := group.NewDefaultIDGenerator()
	timeProvider := group.NewDefaultTimeProvider()
	stringUtils := group.NewDefaultStringUtils()

	family := group.NewUniversalGroup(config, idGen, timeProvider, stringUtils)
	family.Name = "The Smith Family"
	family.Type = group.GroupTypeFamily
	family.DisplayInfo.Description = "Our wonderful family"
	family.SetInitialState()

	// Add family members
	family.AddMember("user-dad", group.MemberTypeUser, "PARENT")
	family.AddMember("user-mom", group.MemberTypeUser, "PARENT")
	family.AddMember("user-son", group.MemberTypeUser, "CHILD")
	family.AddMember("user-daughter", group.MemberTypeUser, "CHILD")

	// Add custom extensions for family-specific data
	family.SetExtension("home_address", "123 Main St")
	family.SetExtension("family_motto", "Together we are stronger")
	family.SetExtension("established_year", 2010)

	fmt.Printf("Created family: %s with %d members\n", family.Name, family.GetMemberCount())
}

// Example 5: Using Extensions for Integration
func IntegrateWithSlack() {
	config := group.DefaultGroupConfig()
	idGen := group.NewDefaultIDGenerator()
	timeProvider := group.NewDefaultTimeProvider()
	stringUtils := group.NewDefaultStringUtils()

	team := group.NewUniversalGroup(config, idGen, timeProvider, stringUtils)
	team.Name = "DevOps Team"
	team.Type = group.GroupTypeTeam
	team.SetInitialState()

	// Set Slack integration
	team.Integrations.Slack = &group.SlackIntegration{
		DisplayID:         "SLACK-CHANNEL-ID-123",
		OnDutyDisplayID:   "SLACK-USER-ONCALL",
		OnDutyDisplayName: "John Doe",
		Emoji:             ":rocket:",
		AdditionalRecipients: []string{
			"SLACK-USER-1",
			"SLACK-USER-2",
		},
	}

	// Add other custom integrations
	if team.Integrations.Custom == nil {
		team.Integrations.Custom = make(map[string]interface{})
	}
	team.Integrations.Custom["pagerduty"] = map[string]interface{}{
		"service_id":        "PD-SERVICE-123",
		"escalation_policy": "POL-456",
	}

	team.SetExtension("monitoring_dashboard", "https://grafana.company.com/d/team-devops")

	fmt.Printf("Team %s integrated with:\n", team.Name)
	fmt.Printf("  - Slack: %s\n", team.Integrations.Slack.DisplayID)
	fmt.Printf("  - PagerDuty: %v\n", team.Integrations.Custom["pagerduty"])
}

// Example 6: Member Management
func ManageMembers() {
	var err error

	config := group.DefaultGroupConfig()
	idGen := group.NewDefaultIDGenerator()
	timeProvider := group.NewDefaultTimeProvider()
	stringUtils := group.NewDefaultStringUtils()

	team := group.NewUniversalGroup(config, idGen, timeProvider, stringUtils)
	team.Name = "Backend Team"
	team.Type = group.GroupTypeTeam
	team.SetInitialState()

	// Add members
	team.AddMember("user-1", group.MemberTypeUser, group.MemberRoleMember)
	team.AddMember("user-2", group.MemberTypeUser, group.MemberRoleMember)
	team.AddMember("user-3", group.MemberTypeUser, group.MemberRoleMember)

	fmt.Printf("Total members: %d\n", team.GetMemberCount())
	fmt.Printf("User members: %v\n", team.GetUserMemberIDs())

	// Update member role
	if team, err = team.UpdateMemberRole("user-2", group.MemberRoleAdmin); err != nil {
		log.Printf("Failed to update role: %v", err)
	} else {
		fmt.Println("Updated user-2 to admin role")
	}

	// Check if member exists
	if team.HasMember("user-1") {
		fmt.Println("user-1 is a member")
	}

	// Get specific member
	if member, err := team.GetMemberByID("user-1"); err == nil {
		fmt.Printf("Member details: ID=%s, Type=%s, Role=%s\n",
			member.ID, member.Type, member.Role)
	}

	// Remove member
	if team, err = team.RemoveMember("user-3"); err != nil {
		log.Printf("Failed to remove member: %v", err)
	} else {
		fmt.Printf("Removed user-3. New count: %d\n", team.GetMemberCount())
	}
}

func main() {
	fmt.Println("=== Example 1: Simple Team ===")
	CreateSimpleTeam()
	fmt.Println()

	fmt.Println("=== Example 2: Hierarchical Organization ===")
	CreateHierarchicalOrganization()
	fmt.Println()

	fmt.Println("=== Example 3: Status Management ===")
	ManageGroupStatus()
	fmt.Println()

	fmt.Println("=== Example 4: Family Group ===")
	CreateFamilyGroup()
	fmt.Println()

	fmt.Println("=== Example 5: Slack Integration ===")
	IntegrateWithSlack()
	fmt.Println()

	fmt.Println("=== Example 6: Member Management ===")
	ManageMembers()
}
