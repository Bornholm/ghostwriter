package searx

type Instances struct {
	Metadata  Metadata `json:"metadata"`
	Instances map[string]Instance
}

type Metadata struct {
	Timestamp int `json:"timestamp"`
}

type Instance struct {
	Analytics       bool              `json:"analytics"`
	Comments        []any             `json:"comments"`
	AlternativeUrls AlternativeUrls   `json:"alternativeUrls"`
	Main            bool              `json:"main"`
	NetworkType     string            `json:"network_type"`
	HTTP            HTTP              `json:"http"`
	Version         string            `json:"version"`
	GitURL          string            `json:"git_url"`
	Generator       string            `json:"generator"`
	ContactURL      any               `json:"contact_url"`
	DocsURL         string            `json:"docs_url"`
	Timing          Timing            `json:"timing"`
	TLS             TLS               `json:"tls"`
	HTML            HTML              `json:"html"`
	Engines         map[string]Engine `json:"engines"`
	Network         Network           `json:"network"`
	Uptime          Uptime            `json:"uptime"`
}

type AlternativeUrls struct {
}

type HTTP struct {
	StatusCode int    `json:"status_code"`
	Error      any    `json:"error"`
	Grade      string `json:"grade"`
	GradeURL   string `json:"gradeUrl"`
}

type Value struct {
	Value float64 `json:"value"`
}

type Initial struct {
	SuccessPercentage float64 `json:"success_percentage"`
	All               Value   `json:"all"`
	Server            Value   `json:"server"`
}

type Stats struct {
	Median float64 `json:"median"`
	Stdev  float64 `json:"stdev"`
	Mean   float64 `json:"mean"`
}

type Search struct {
	SuccessPercentage float64 `json:"success_percentage"`
	All               Stats   `json:"all"`
	Server            Stats   `json:"server"`
	Load              Stats   `json:"load"`
}

type Timing struct {
	Initial  Initial `json:"initial"`
	Search   Search  `json:"search"`
	SearchGo Search  `json:"search_go"`
}

type Issuer struct {
	CommonName       string `json:"commonName"`
	CountryName      string `json:"countryName"`
	OrganizationName string `json:"organizationName"`
}

type Subject struct {
	CommonName       string `json:"commonName"`
	CountryName      any    `json:"countryName"`
	OrganizationName any    `json:"organizationName"`
	AltName          string `json:"altName"`
}

type Certificate struct {
	Issuer                Issuer   `json:"issuer"`
	Subject               Subject  `json:"subject"`
	Version               int      `json:"version"`
	SerialNumber          string   `json:"serialNumber"`
	NotBefore             string   `json:"notBefore"`
	NotAfter              string   `json:"notAfter"`
	Ocsp                  []string `json:"OCSP"`
	CaIssuers             []string `json:"caIssuers"`
	CrlDistributionPoints []string `json:"crlDistributionPoints"`
	Sha256                string   `json:"sha256"`
	SignatureAlgorithm    string   `json:"signatureAlgorithm"`
}

type TLS struct {
	Version     string      `json:"version"`
	Certificate Certificate `json:"certificate"`
	Grade       string      `json:"grade"`
	GradeURL    string      `json:"gradeUrl"`
}

type Ref struct {
	HashRef int `json:"hashRef"`
}

type Ressources struct {
	CSS          map[string]Ref `json:"css"`
	InlineScript []any          `json:"inline_script"`
	InlineStyle  []any          `json:"inline_style"`
	Link         map[string]Ref `json:"link"`
	Script       map[string]Ref `json:"script"`
}

type HTML struct {
	Ressources Ressources `json:"ressources"`
	Grade      string     `json:"grade"`
}

type Engine struct {
	ErrorRate int   `json:"error_rate"`
	Errors    []int `json:"errors"`
}

type Ip struct {
	Reverse   any    `json:"reverse"`
	FieldType string `json:"field_type"`
	AsnCidr   string `json:"asn_cidr"`
	HTTPSPort bool   `json:"https_port"`
}

type Network struct {
	Ips        map[string]Ip `json:"ips"`
	Ipv6       bool          `json:"ipv6"`
	AsnPrivacy int           `json:"asn_privacy"`
	Dnssec     int           `json:"dnssec"`
}

type Uptime struct {
	UptimeDay   float64 `json:"uptimeDay"`
	UptimeWeek  float64 `json:"uptimeWeek"`
	UptimeMonth float64 `json:"uptimeMonth"`
	UptimeYear  float64 `json:"uptimeYear"`
}
