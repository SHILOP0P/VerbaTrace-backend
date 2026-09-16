package call

import "fmt"

func visibleToUserCondition(callAlias string, userParam string) string {
	return fmt.Sprintf(`
	(
	    %s.uploaded_by_user_uuid = %s
	    OR (
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
	)`, callAlias, userParam, callAlias, callAlias, userParam, callAlias, callAlias, userParam)
}

func VisibleToUserConditionForFolders(callAlias string, userParam string) string {
	return visibleToUserCondition(callAlias, userParam)
}
