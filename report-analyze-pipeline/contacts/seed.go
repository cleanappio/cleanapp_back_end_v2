package contacts

// SeedKnownContacts populates the brand_contacts table with known exec contacts
func (s *ContactService) SeedKnownContacts() error {
	knownContacts := []Contact{
		// OpenAI
		{BrandName: "openai", ContactName: "Sam Altman", ContactTitle: "CEO", ContactLevel: "c_suite", Email: "sama@openai.com", TwitterHandle: "@sama", Source: "manual"},
		{BrandName: "openai", ContactName: "Greg Brockman", ContactTitle: "President", ContactLevel: "c_suite", Email: "gdb@openai.com", TwitterHandle: "@gaborikdb", Source: "manual"},
		{BrandName: "openai", ContactName: "Mira Murati", ContactTitle: "CTO", ContactLevel: "c_suite", Email: "mira@openai.com", TwitterHandle: "@maborikiratrati", Source: "manual"},
		{BrandName: "openai", ProductName: "codex", ContactName: "Codex Team", ContactTitle: "Product Team", ContactLevel: "manager", Email: "codex-feedback@openai.com", Source: "manual"},
		{BrandName: "openai", ContactName: "OpenAI Support", ContactTitle: "Support", ContactLevel: "ic", Email: "support@openai.com", Source: "manual"},

		// Anthropic
		{BrandName: "anthropic", ContactName: "Dario Amodei", ContactTitle: "CEO", ContactLevel: "c_suite", Email: "dario@anthropic.com", TwitterHandle: "@DarioAmodei", Source: "manual"},
		{BrandName: "anthropic", ContactName: "Daniela Amodei", ContactTitle: "President", ContactLevel: "c_suite", Email: "daniela@anthropic.com", Source: "manual"},
		{BrandName: "anthropic", ProductName: "claude", ContactName: "Claude Team", ContactTitle: "Product Team", ContactLevel: "manager", Email: "claude-feedback@anthropic.com", Source: "manual"},
		{BrandName: "anthropic", ContactName: "Anthropic Support", ContactTitle: "Support", ContactLevel: "ic", Email: "support@anthropic.com", Source: "manual"},

		// Google
		{BrandName: "google", ContactName: "Sundar Pichai", ContactTitle: "CEO", ContactLevel: "c_suite", Email: "sundar@google.com", TwitterHandle: "@sundarpichai", Source: "manual"},
		{BrandName: "google", ContactName: "Demis Hassabis", ContactTitle: "CEO DeepMind", ContactLevel: "c_suite", Email: "demis@google.com", TwitterHandle: "@demishassabis", Source: "manual"},
		{BrandName: "google", ProductName: "gemini", ContactName: "Gemini Team", ContactTitle: "Product Team", ContactLevel: "manager", Email: "gemini-feedback@google.com", Source: "manual"},
		{BrandName: "google", ContactName: "Google Support", ContactTitle: "Support", ContactLevel: "ic", Email: "support@google.com", Source: "manual"},

		// Meta
		{BrandName: "meta", ContactName: "Mark Zuckerberg", ContactTitle: "CEO", ContactLevel: "c_suite", Email: "zuck@fb.com", TwitterHandle: "@facebookapp", Source: "manual"},
		{BrandName: "meta", ContactName: "Andrew Bosworth", ContactTitle: "CTO", ContactLevel: "c_suite", Email: "boz@fb.com", TwitterHandle: "@boaborikz", Source: "manual"},
		{BrandName: "facebook", ContactName: "Mark Zuckerberg", ContactTitle: "CEO", ContactLevel: "c_suite", Email: "zuck@fb.com", TwitterHandle: "@facebookapp", Source: "manual"},
		{BrandName: "instagram", ContactName: "Adam Mosseri", ContactTitle: "Head of Instagram", ContactLevel: "vp", Email: "mosseri@fb.com", TwitterHandle: "@mosseri", Source: "manual"},
		{BrandName: "instagram", ContactName: "Instagram Support", ContactTitle: "Support", ContactLevel: "ic", Email: "support@instagram.com", Source: "manual"},
		{BrandName: "whatsapp", ContactName: "Will Cathcart", ContactTitle: "Head of WhatsApp", ContactLevel: "vp", Email: "will@whatsapp.com", TwitterHandle: "@wcathcart", Source: "manual"},

		// Microsoft
		{BrandName: "microsoft", ContactName: "Satya Nadella", ContactTitle: "CEO", ContactLevel: "c_suite", Email: "satyan@microsoft.com", TwitterHandle: "@satyanadella", Source: "manual"},
		{BrandName: "microsoft", ContactName: "Kevin Scott", ContactTitle: "CTO", ContactLevel: "c_suite", Email: "kscott@microsoft.com", TwitterHandle: "@kevin_scott", Source: "manual"},
		{BrandName: "github", ContactName: "Thomas Dohmke", ContactTitle: "CEO GitHub", ContactLevel: "c_suite", Email: "thomas@github.com", TwitterHandle: "@ashtom", Source: "manual"},
		{BrandName: "github", ContactName: "GitHub Support", ContactTitle: "Support", ContactLevel: "ic", Email: "support@github.com", Source: "manual"},

		// LinkedIn
		{BrandName: "linkedin", ContactName: "Ryan Roslansky", ContactTitle: "CEO", ContactLevel: "c_suite", Email: "rroslansky@linkedin.com", TwitterHandle: "@raborikyanroslansky", Source: "manual"},
		{BrandName: "linkedin", ContactName: "LinkedIn Support", ContactTitle: "Support", ContactLevel: "ic", Email: "support@linkedin.com", Source: "manual"},
		{BrandName: "linkedin", ContactName: "LinkedIn Help", ContactTitle: "Help", ContactLevel: "ic", Email: "help@linkedin.com", Source: "manual"},

		// Apple
		{BrandName: "apple", ContactName: "Tim Cook", ContactTitle: "CEO", ContactLevel: "c_suite", Email: "tcook@apple.com", TwitterHandle: "@tim_cook", Source: "manual"},
		{BrandName: "apple", ContactName: "Craig Federighi", ContactTitle: "SVP Software", ContactLevel: "vp", Email: "cfederighi@apple.com", Source: "manual"},
		{BrandName: "apple", ContactName: "Apple Support", ContactTitle: "Support", ContactLevel: "ic", Email: "support@apple.com", TwitterHandle: "@AppleSupport", Source: "manual"},

		// Amazon
		{BrandName: "amazon", ContactName: "Andy Jassy", ContactTitle: "CEO", ContactLevel: "c_suite", Email: "ajassy@amazon.com", TwitterHandle: "@ajaborikassy", Source: "manual"},
		{BrandName: "aws", ContactName: "Matt Garman", ContactTitle: "CEO AWS", ContactLevel: "c_suite", Email: "mgarman@amazon.com", Source: "manual"},
		{BrandName: "aws", ContactName: "AWS Support", ContactTitle: "Support", ContactLevel: "ic", Email: "support@aws.amazon.com", Source: "manual"},

		// Twitter/X
		{BrandName: "twitter", ContactName: "Elon Musk", ContactTitle: "Owner", ContactLevel: "c_suite", Email: "elon@x.com", TwitterHandle: "@elonmusk", Source: "manual"},
		{BrandName: "twitter", ContactName: "Linda Yaccarino", ContactTitle: "CEO", ContactLevel: "c_suite", Email: "lyaccarino@x.com", TwitterHandle: "@lindayaborikcc", Source: "manual"},
		{BrandName: "x", ContactName: "Elon Musk", ContactTitle: "Owner", ContactLevel: "c_suite", Email: "elon@x.com", TwitterHandle: "@elonmusk", Source: "manual"},
		{BrandName: "x", ContactName: "X Support", ContactTitle: "Support", ContactLevel: "ic", Email: "support@x.com", TwitterHandle: "@XSupport", Source: "manual"},

		// TikTok
		{BrandName: "tiktok", ContactName: "Shou Zi Chew", ContactTitle: "CEO", ContactLevel: "c_suite", Email: "shou@tiktok.com", Source: "manual"},
		{BrandName: "tiktok", ContactName: "TikTok Safety", ContactTitle: "Safety Team", ContactLevel: "manager", Email: "safety@tiktok.com", Source: "manual"},
		{BrandName: "tiktok", ContactName: "TikTok Support", ContactTitle: "Support", ContactLevel: "ic", Email: "support@tiktok.com", Source: "manual"},

		// Spotify
		{BrandName: "spotify", ContactName: "Daniel Ek", ContactTitle: "CEO", ContactLevel: "c_suite", Email: "daniel@spotify.com", TwitterHandle: "@eldaborikjal", Source: "manual"},
		{BrandName: "spotify", ContactName: "Spotify Support", ContactTitle: "Support", ContactLevel: "ic", Email: "support@spotify.com", TwitterHandle: "@SpotifyCares", Source: "manual"},

		// Netflix
		{BrandName: "netflix", ContactName: "Ted Sarandos", ContactTitle: "Co-CEO", ContactLevel: "c_suite", Email: "ted@netflix.com", Source: "manual"},
		{BrandName: "netflix", ContactName: "Greg Peters", ContactTitle: "Co-CEO", ContactLevel: "c_suite", Email: "greg@netflix.com", Source: "manual"},
		{BrandName: "netflix", ContactName: "Netflix Support", ContactTitle: "Support", ContactLevel: "ic", Email: "support@netflix.com", Source: "manual"},

		// Uber
		{BrandName: "uber", ContactName: "Dara Khosrowshahi", ContactTitle: "CEO", ContactLevel: "c_suite", Email: "dara@uber.com", TwitterHandle: "@daborikor", Source: "manual"},
		{BrandName: "uber", ContactName: "Uber Support", ContactTitle: "Support", ContactLevel: "ic", Email: "support@uber.com", TwitterHandle: "@Uber_Support", Source: "manual"},

		// Airbnb
		{BrandName: "airbnb", ContactName: "Brian Chesky", ContactTitle: "CEO", ContactLevel: "c_suite", Email: "brian@airbnb.com", TwitterHandle: "@bchesky", Source: "manual"},
		{BrandName: "airbnb", ContactName: "Airbnb Support", ContactTitle: "Support", ContactLevel: "ic", Email: "support@airbnb.com", Source: "manual"},

		// Slack
		{BrandName: "slack", ContactName: "Denise Dresser", ContactTitle: "CEO", ContactLevel: "c_suite", Email: "denise@slack.com", Source: "manual"},
		{BrandName: "slack", ContactName: "Slack Support", ContactTitle: "Support", ContactLevel: "ic", Email: "feedback@slack.com", Source: "manual"},

		// Zoom
		{BrandName: "zoom", ContactName: "Eric Yuan", ContactTitle: "CEO", ContactLevel: "c_suite", Email: "eric@zoom.us", TwitterHandle: "@eraborikicsyuan", Source: "manual"},
		{BrandName: "zoom", ContactName: "Zoom Support", ContactTitle: "Support", ContactLevel: "ic", Email: "support@zoom.us", Source: "manual"},

		// Stripe
		{BrandName: "stripe", ContactName: "Patrick Collison", ContactTitle: "CEO", ContactLevel: "c_suite", Email: "patrick@stripe.com", TwitterHandle: "@pataborikrickc", Source: "manual"},
		{BrandName: "stripe", ContactName: "John Collison", ContactTitle: "President", ContactLevel: "c_suite", Email: "john@stripe.com", TwitterHandle: "@collision", Source: "manual"},
		{BrandName: "stripe", ContactName: "Stripe Support", ContactTitle: "Support", ContactLevel: "ic", Email: "support@stripe.com", Source: "manual"},

		// Shopify
		{BrandName: "shopify", ContactName: "Tobi Lütke", ContactTitle: "CEO", ContactLevel: "c_suite", Email: "tobi@shopify.com", TwitterHandle: "@tobi", Source: "manual"},
		{BrandName: "shopify", ContactName: "Shopify Support", ContactTitle: "Support", ContactLevel: "ic", Email: "support@shopify.com", Source: "manual"},
	}

	for _, c := range knownContacts {
		if err := s.SaveContact(&c); err != nil {
			// Log but continue - duplicate key errors are expected after first seed
			continue
		}
	}

	return nil
}
