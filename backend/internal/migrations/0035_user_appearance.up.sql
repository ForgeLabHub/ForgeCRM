-- A user's explicit visual choices are independent of roles and organization data.
-- A NULL style means recompute the default from the active organization's roles.
CREATE TABLE user_appearance_preferences (
    user_id uuid PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    style text CHECK (style IS NULL OR style IN ('default', 'minimal', 'midnight', 'glass', 'premium', 'brutalist', 'cyberpunk', 'soft', 'editorial', 'warm', 'colorful')),
    mode text NOT NULL CHECK (mode IN ('light', 'dark', 'system')),
    accent text NOT NULL CHECK (accent IN ('theme', 'indigo', 'emerald', 'amber', 'rose', 'cyan')),
    density text NOT NULL CHECK (density IN ('compact', 'standard', 'spacious')),
    updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE TRIGGER user_appearance_preferences_updated_at BEFORE UPDATE ON user_appearance_preferences
    FOR EACH ROW EXECUTE FUNCTION set_updated_at();
