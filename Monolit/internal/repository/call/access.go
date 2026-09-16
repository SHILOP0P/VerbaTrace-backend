package call

import "fmt"

// visibleToUserCondition is the one place that decides who sees a call: the
// person who uploaded it, the owner and the deputy of its company, and the
// leader of its department. A call in the bin is hidden from every normal read.
func visibleToUserCondition(callAlias string, userParam string) string {
	return fmt.Sprintf(`
	(
	    %s.deleted_at IS NULL
	    AND %s
	)`, callAlias, membershipReachCondition(callAlias, userParam))
}

// VisibleToUserCondition is the same predicate for every other package that has
// to list calls: reports, search, folders and the assistant must never show a
// call the calls list itself would hide.
func VisibleToUserCondition(callAlias string, userParam string) string {
	return visibleToUserCondition(callAlias, userParam)
}

// deletableByUserCondition is stricter than visibility: an employee cannot wipe
// a company call, that is the job of the department leader, the deputy or the
// owner. A personal call belongs to whoever uploaded it.
func deletableByUserCondition(callAlias string, userParam string) string {
	return fmt.Sprintf(`
	(
	    %s.deleted_at IS NULL
	    AND %s
	)`, callAlias, managementReachCondition(callAlias, userParam))
}

// restorableByUserCondition mirrors deletion rights for the bin.
func restorableByUserCondition(callAlias string, userParam string) string {
	return fmt.Sprintf(`
	(
	    %s.deleted_at IS NOT NULL
	    AND %s
	)`, callAlias, managementReachCondition(callAlias, userParam))
}

// deletedVisibleToUserCondition lists the bin for the people who may restore it.
func deletedVisibleToUserCondition(callAlias string, userParam string) string {
	return restorableByUserCondition(callAlias, userParam)
}

func membershipReachCondition(callAlias string, userParam string) string {
	return fmt.Sprintf(`(
	    %s.uploaded_by_user_uuid = %s
	    OR %s
	)`, callAlias, userParam, scopeManagementCondition(callAlias, userParam))
}

func managementReachCondition(callAlias string, userParam string) string {
	return fmt.Sprintf(`(
	    (%s.company_uuid IS NULL AND %s.uploaded_by_user_uuid = %s)
	    OR %s
	)`, callAlias, callAlias, userParam, scopeManagementCondition(callAlias, userParam))
}

func scopeManagementCondition(callAlias string, userParam string) string {
	return fmt.Sprintf(`(
	    (
	        %s.company_uuid IS NOT NULL
	        AND EXISTS (
	            SELECT 1
	            FROM company_members cm
	            WHERE cm.company_uuid = %s.company_uuid
	              AND cm.user_uuid = %s
	              AND cm.role IN ('company_manager','company_deputy')
	              AND cm.status = 'active'
	        )
	    )
	    OR (
	        %s.department_uuid IS NOT NULL
	        AND EXISTS (
	            SELECT 1
	            FROM department_members dm
	            WHERE dm.department_uuid = %s.department_uuid
	              AND dm.user_uuid = %s
	              AND dm.role = 'department_leader'
	              AND dm.status = 'active'
	        )
	    )
	)`, callAlias, callAlias, userParam, callAlias, callAlias, userParam)
}
