package gh

import "strings"

// TokenType is the kind of GitHub credential in use.
//
// It matters because the same 403 means different things. On a classic
// token it is a missing scope; on a fine-grained one it is a missing
// permission, or an organisation that has not approved the token yet, or
// a repository left out of its selected set. Telling someone to "check
// the scopes" when they are holding a fine-grained token sends them
// looking at a screen that does not exist.
type TokenType string

const (
	TokenClassic     TokenType = "classic"
	TokenFineGrained TokenType = "fine-grained"
	TokenOAuth       TokenType = "oauth"       // what `gh auth login` produces
	TokenAppUser     TokenType = "app-user"    // GitHub App, acting as a user
	TokenAppInstall  TokenType = "app-install" // GitHub App installation
	TokenUnknown     TokenType = "unknown"
)

// DetectTokenType reads the type from the token's prefix. GitHub gives
// each kind a distinct one, so this is exact rather than a guess - except
// for older tokens issued before the prefixes existed, which come back
// unknown.
func DetectTokenType(token string) TokenType {
	switch {
	case strings.HasPrefix(token, "github_pat_"):
		return TokenFineGrained
	case strings.HasPrefix(token, "ghp_"):
		return TokenClassic
	case strings.HasPrefix(token, "gho_"):
		return TokenOAuth
	case strings.HasPrefix(token, "ghu_"):
		return TokenAppUser
	case strings.HasPrefix(token, "ghs_"):
		return TokenAppInstall
	default:
		return TokenUnknown
	}
}

// Label is how the type reads in a message.
func (t TokenType) Label() string {
	switch t {
	case TokenClassic:
		return "classic personal access token"
	case TokenFineGrained:
		return "fine-grained personal access token"
	case TokenOAuth:
		return "gh CLI login"
	case TokenAppUser:
		return "GitHub App user token"
	case TokenAppInstall:
		return "GitHub App installation token"
	}
	return "token"
}

// permissionAdvice is what to check when GitHub refuses, phrased for the
// kind of credential actually in use.
func (t TokenType) permissionAdvice() string {
	switch t {
	case TokenFineGrained:
		return "For a fine-grained token, check three things: the permission is granted, " +
			"the organisation has approved the token (org-owned ones need an admin to approve " +
			"before they work), and the repository is in the token's selected set."
	case TokenClassic:
		return "For a classic token, check the scopes: repo, read:org, and security_events " +
			"for the alert counts."
	case TokenOAuth:
		return "This is the token from `gh auth login`, which carries whatever scopes gh asked " +
			"for. Run `gh auth refresh -s read:org,security_events`, or create a dedicated token."
	case TokenAppInstall:
		return "A GitHub App installation token is scoped to the app's installation, and cannot " +
			"see anything personal such as your own review requests."
	case TokenAppUser:
		return "A GitHub App user token can reach only what BOTH you and the installation can " +
			"reach. Check the App is installed on this organisation, and on all repositories " +
			"rather than a selected few - a partial installation makes Argus under-report " +
			"with no error."
	}
	return "Check that the token grants access to this resource."
}
