create or replace function test.get_user_count() returns bigint security definer as
$_SCHEMAHERO_$
DECLARE
    user_count bigint;
BEGIN
    SELECT COUNT(*) INTO user_count FROM users;
    RETURN user_count;
END;
$_SCHEMAHERO_$
language PLpgSQL;
