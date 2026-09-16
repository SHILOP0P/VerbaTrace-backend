package company

import "fmt"

// ManagementRolesSQL lists the roles that run a company day to day. The deputy
// shares the owner's rights except the owner-only actions, so every access
// predicate must accept both roles instead of hard-coding the owner.
const ManagementRolesSQL = "('company_manager','company_deputy')"

// ManagesCompanyCondition builds the SQL predicate for "this user runs that
// company". Callers pass the parameter placeholders they already use.
func ManagesCompanyCondition(companyParam string, userParam string) string {
	return fmt.Sprintf(`EXISTS (
	    SELECT 1
	    FROM company_members cm
	    WHERE cm.company_uuid = %s
	      AND cm.user_uuid = %s
	      AND cm.status = 'active'
	      AND cm.role IN %s
	)`, companyParam, userParam, ManagementRolesSQL)
}
