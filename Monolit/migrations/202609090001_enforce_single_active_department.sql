-- +goose Up
ALTER TABLE department_members ADD COLUMN company_uuid UUID;
UPDATE department_members dm SET company_uuid=d.company_uuid
FROM departments d WHERE d.department_uuid=dm.department_uuid;
ALTER TABLE department_members ALTER COLUMN company_uuid SET NOT NULL;
ALTER TABLE department_members ADD CONSTRAINT fk_department_members_company
FOREIGN KEY(department_uuid,company_uuid) REFERENCES departments(department_uuid,company_uuid);
-- Fail on historical conflicts rather than silently revoking someone's access.
CREATE UNIQUE INDEX uq_department_members_one_active_company
ON department_members(company_uuid,user_uuid) WHERE status='active';

-- +goose StatementBegin
CREATE FUNCTION set_department_members_company() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    SELECT company_uuid INTO NEW.company_uuid FROM departments WHERE department_uuid=NEW.department_uuid;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd
CREATE TRIGGER department_members_company_identity
BEFORE INSERT OR UPDATE ON department_members
FOR EACH ROW EXECUTE FUNCTION set_department_members_company();

-- +goose Down
DROP TRIGGER department_members_company_identity ON department_members;
DROP FUNCTION set_department_members_company();
DROP INDEX uq_department_members_one_active_company;
ALTER TABLE department_members DROP CONSTRAINT fk_department_members_company;
ALTER TABLE department_members DROP COLUMN company_uuid;
