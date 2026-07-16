CREATE FUNCTION benchmark_force_unlogged()
RETURNS event_trigger
LANGUAGE plpgsql
AS $$
DECLARE
    command record;
    primary_key_name text;
BEGIN
    FOR command IN
        SELECT *
        FROM pg_event_trigger_ddl_commands()
        WHERE object_type = 'table'
    LOOP
        EXECUTE format('ALTER TABLE %s SET UNLOGGED', command.object_identity);

        SELECT conname
        INTO primary_key_name
        FROM pg_constraint
        WHERE conrelid = command.objid
          AND contype = 'p';

        IF primary_key_name IS NOT NULL THEN
            EXECUTE format(
                'ALTER TABLE %s DROP CONSTRAINT %I',
                command.object_identity,
                primary_key_name
            );
        END IF;
    END LOOP;
END;
$$;

CREATE EVENT TRIGGER benchmark_force_unlogged
    ON ddl_command_end
    WHEN TAG IN ('CREATE TABLE')
    EXECUTE FUNCTION benchmark_force_unlogged();

CREATE FUNCTION benchmark_drop_added_primary_key()
RETURNS event_trigger
LANGUAGE plpgsql
AS $$
DECLARE
    command record;
BEGIN
    FOR command IN
        SELECT
            constraint_row.conrelid::regclass::text AS table_identity,
            constraint_row.conname
        FROM pg_constraint AS constraint_row
        JOIN pg_class AS table_row
          ON table_row.oid = constraint_row.conrelid
        JOIN pg_namespace AS namespace_row
          ON namespace_row.oid = table_row.relnamespace
        WHERE constraint_row.contype = 'p'
          AND namespace_row.nspname NOT IN ('pg_catalog', 'information_schema')
    LOOP
        EXECUTE format(
            'ALTER TABLE %s DROP CONSTRAINT %I',
            command.table_identity,
            command.conname
        );
    END LOOP;
END;
$$;

CREATE EVENT TRIGGER benchmark_drop_added_primary_key
    ON ddl_command_end
    WHEN TAG IN ('ALTER TABLE')
    EXECUTE FUNCTION benchmark_drop_added_primary_key();
