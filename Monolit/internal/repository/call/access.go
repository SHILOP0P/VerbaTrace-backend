package call

import "fmt"

// visibleToUserCondition is the one place that decides who sees a call: the
// person who uploaded it, the owner and the deputy of its company, the leader of
// its department, and an employee of the company a person or a system of record
// marked in the call. A call in the bin is hidden from every normal read.
func visibleToUserCondition(callAlias string, userParam string) string {
	return fmt.Sprintf(`
	(
	    %s.deleted_at IS NULL
	    AND (%s OR %s)
	)`, callAlias, membershipReachCondition(callAlias, userParam), markedSubjectCondition(callAlias, userParam))
}

// VisibleToUserCondition is the same predicate for every other package that has
// to list calls: reports, search, folders and the assistant must never show a
// call the calls list itself would hide.
func VisibleToUserCondition(callAlias string, userParam string) string {
	return visibleToUserCondition(callAlias, userParam)
}

// editableByUserCondition is who may change a call: the circle that saw it
// before employees could be marked in it. Seeing a call is not enough to edit
// its transcript or speakers — a marked employee could otherwise mark others
// and hand the call on.
func editableByUserCondition(callAlias string, userParam string) string {
	return fmt.Sprintf(`
	(
	    %s.deleted_at IS NULL
	    AND %s
	)`, callAlias, membershipReachCondition(callAlias, userParam))
}

// EditableByUserCondition is editableByUserCondition for other packages.
func EditableByUserCondition(callAlias string, userParam string) string {
	return editableByUserCondition(callAlias, userParam)
}

// markedSubjectCondition lets an employee read a company call they were marked
// in. Membership is checked on every read, so leaving the company or losing the
// mark takes the call away at once.
func markedSubjectCondition(callAlias string, userParam string) string {
	return fmt.Sprintf(`(
	    %s.company_uuid IS NOT NULL
	    AND EXISTS (
	        SELECT 1
	        FROM call_subjects cs
	        JOIN company_members sm ON sm.company_uuid = %s.company_uuid AND sm.user_uuid = cs.user_uuid AND sm.status = 'active'
	        WHERE cs.call_uuid = %s.call_uuid
	          AND cs.user_uuid = %s
	          AND cs.grants_access
	    )
	)`, callAlias, callAlias, callAlias, userParam)
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
