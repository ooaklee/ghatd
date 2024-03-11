package toolbox

// ExampleEmailAddressDomainRegex regex pattern for the an example email address domain
const ExampleEmailAddressDomainRegex string = ".*@example.io$"

// UuidV4Regex regex pattern for Uuidv4
const UuidV4Regex string = "^[a-fA-F0-9]{8}-[a-fA-F0-9]{4}-4[a-fA-F0-9]{3}-[8|9|aA|bB][a-fA-F0-9]{3}-[a-fA-F0-9]{12}$"

// TimeNowUTCAsStringRegex basic regex for formatted time as string
const TimeNowUTCAsStringRegex string = "^[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2}$"

// EmailRegex regex patter for email
const EmailRegex string = "^[a-zA-Z0-9.!#$%&'*+\\/=?^_`{|}~-]+@[a-zA-Z0-9](?:[a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?(?:\\.[a-zA-Z0-9](?:[a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?)*$"

// Base64EncodedRegex is pattern to identify if something is base64 format
// Another regex: "^(?:[A-Za-z0-9+/]{4})*(?:[A-Za-z0-9+/]{2}==|[A-Za-z0-9+/]{3}=|[A-Za-z0-9+/]{4})$"
const Base64EncodedRegex string = "^([A-Za-z0-9+/]{4})*([A-Za-z0-9+/]{3}=|[A-Za-z0-9+/]{2}==)?$"

// UserRoleIdSuffixRegex is the pattern used when versioning the different roles (from payment integrators)
const UserRoleIdSuffixRegex string = "(__[0-9]+)"
