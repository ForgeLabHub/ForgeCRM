-- Never silently discard user preferences during a rollback.
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM user_appearance_preferences) THEN
        RAISE EXCEPTION 'cannot roll back appearance preferences while saved choices exist';
    END IF;
    DROP TABLE user_appearance_preferences;
END;
$$;
