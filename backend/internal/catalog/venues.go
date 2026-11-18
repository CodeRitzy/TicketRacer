// Package catalog holds the real-world reference data: US states, real music venues
// (commonly cited capacities, kind, seating sections, source URL), price tiers,
// seat layouts and the invented-act generator. Compiled in: no runtime web calls.
package catalog

// Kind decides the price tier, the generic section names and the venue map shape.
type Kind string

const (
	Club         Kind = "club"
	Ballroom     Kind = "ballroom"
	Theater      Kind = "theater"
	Amphitheater Kind = "amphitheater"
	Arena        Kind = "arena"
)

// Venue is one real venue. Capacity is the commonly cited concert figure (sources vary by
// configuration); Sections run nearest-the-stage first, empty means the kind's generic layout.
type Venue struct {
	Key      string   `json:"key"`
	Name     string   `json:"name"`
	City     string   `json:"city"`
	State    string   `json:"state"`
	Capacity int      `json:"capacity"`
	Kind     Kind     `json:"kind"`
	Sections []string `json:"-"`
	Source   string   `json:"-"`
}

// State is a US state (or DC) with the IANA zone used for show times and the browser guess.
type State struct {
	Code string `json:"code"`
	Name string `json:"name"`
	Zone string `json:"zone"`
}

// States lists all 50 states plus DC.
var States = []State{
	{"AL", "Alabama", "America/Chicago"}, {"AK", "Alaska", "America/Anchorage"}, {"AZ", "Arizona", "America/Phoenix"},
	{"AR", "Arkansas", "America/Chicago"}, {"CA", "California", "America/Los_Angeles"}, {"CO", "Colorado", "America/Denver"},
	{"CT", "Connecticut", "America/New_York"}, {"DE", "Delaware", "America/New_York"}, {"DC", "District of Columbia", "America/New_York"},
	{"FL", "Florida", "America/New_York"}, {"GA", "Georgia", "America/New_York"}, {"HI", "Hawaii", "Pacific/Honolulu"},
	{"ID", "Idaho", "America/Boise"}, {"IL", "Illinois", "America/Chicago"}, {"IN", "Indiana", "America/Indiana/Indianapolis"},
	{"IA", "Iowa", "America/Chicago"}, {"KS", "Kansas", "America/Chicago"}, {"KY", "Kentucky", "America/Kentucky/Louisville"},
	{"LA", "Louisiana", "America/Chicago"}, {"ME", "Maine", "America/New_York"}, {"MD", "Maryland", "America/New_York"},
	{"MA", "Massachusetts", "America/New_York"}, {"MI", "Michigan", "America/Detroit"}, {"MN", "Minnesota", "America/Chicago"},
	{"MS", "Mississippi", "America/Chicago"}, {"MO", "Missouri", "America/Chicago"}, {"MT", "Montana", "America/Denver"},
	{"NE", "Nebraska", "America/Chicago"}, {"NV", "Nevada", "America/Los_Angeles"}, {"NH", "New Hampshire", "America/New_York"},
	{"NJ", "New Jersey", "America/New_York"}, {"NM", "New Mexico", "America/Denver"}, {"NY", "New York", "America/New_York"},
	{"NC", "North Carolina", "America/New_York"}, {"ND", "North Dakota", "America/Chicago"}, {"OH", "Ohio", "America/New_York"},
	{"OK", "Oklahoma", "America/Chicago"}, {"OR", "Oregon", "America/Los_Angeles"}, {"PA", "Pennsylvania", "America/New_York"},
	{"RI", "Rhode Island", "America/New_York"}, {"SC", "South Carolina", "America/New_York"}, {"SD", "South Dakota", "America/Chicago"},
	{"TN", "Tennessee", "America/Chicago"}, {"TX", "Texas", "America/Chicago"}, {"UT", "Utah", "America/Denver"},
	{"VT", "Vermont", "America/New_York"}, {"VA", "Virginia", "America/New_York"}, {"WA", "Washington", "America/Los_Angeles"},
	{"WV", "West Virginia", "America/New_York"}, {"WI", "Wisconsin", "America/Chicago"}, {"WY", "Wyoming", "America/Denver"},
}

const wiki = "https://en.wikipedia.org/wiki/"

// Venues: 1–4 well-known music venues per state. Capacities are the commonly cited concert
// figures; each Source is where to audit the number.
var Venues = []Venue{
	{"orion-amphitheater", "Orion Amphitheater", "Huntsville", "AL", 8000, Amphitheater, nil, wiki + "Orion_Amphitheater"},
	{"iron-city", "Iron City", "Birmingham", "AL", 1300, Ballroom, nil, "https://www.ironcitybham.com/"},
	{"atwood-concert-hall", "Atwood Concert Hall", "Anchorage", "AK", 2000, Theater, nil, wiki + "Alaska_Center_for_the_Performing_Arts"},
	{"arizona-financial-theatre", "Arizona Financial Theatre", "Phoenix", "AZ", 5000, Theater, nil, wiki + "Arizona_Financial_Theatre"},
	{"the-van-buren", "The Van Buren", "Phoenix", "AZ", 1800, Ballroom, nil, "https://www.thevanburenphx.com/"},
	{"rialto-theatre-tucson", "Rialto Theatre", "Tucson", "AZ", 1200, Ballroom, nil, wiki + "Rialto_Theatre_(Tucson,_Arizona)"},
	{"walmart-amp", "Walmart AMP", "Rogers", "AR", 11000, Amphitheater, nil, wiki + "Walmart_Arkansas_Music_Pavilion"},
	{"robinson-center", "Robinson Center", "Little Rock", "AR", 2200, Theater, nil, wiki + "Robinson_Center"},
	{"hollywood-bowl", "Hollywood Bowl", "Los Angeles", "CA", 17500, Amphitheater, []string{"Pool Circle", "Garden Boxes", "Terrace Boxes", "Benches"}, wiki + "Hollywood_Bowl"},
	{"the-fillmore-sf", "The Fillmore", "San Francisco", "CA", 1315, Ballroom, []string{"Floor", "Balcony"}, wiki + "The_Fillmore"},
	{"greek-theatre-la", "Greek Theatre", "Los Angeles", "CA", 5900, Amphitheater, nil, wiki + "Greek_Theatre_(Los_Angeles)"},
	{"red-rocks", "Red Rocks Amphitheatre", "Morrison", "CO", 9525, Amphitheater, []string{"Front Rows", "Center Rows", "Upper Rows"}, wiki + "Red_Rocks_Amphitheatre"},
	{"fillmore-auditorium-denver", "Fillmore Auditorium", "Denver", "CO", 3700, Ballroom, nil, wiki + "Fillmore_Auditorium_(Denver)"},
	{"mission-ballroom", "Mission Ballroom", "Denver", "CO", 3950, Ballroom, nil, wiki + "Mission_Ballroom"},
	{"toyota-oakdale-theatre", "Toyota Oakdale Theatre", "Wallingford", "CT", 4800, Theater, nil, wiki + "Oakdale_Theatre"},
	{"college-street-music-hall", "College Street Music Hall", "New Haven", "CT", 2000, Ballroom, nil, wiki + "College_Street_Music_Hall"},
	{"the-queen-wilmington", "The Queen", "Wilmington", "DE", 1000, Club, nil, wiki + "The_Queen_(Wilmington,_Delaware)"},
	{"the-anthem", "The Anthem", "Washington", "DC", 6000, Ballroom, nil, wiki + "The_Anthem_(music_venue)"},
	{"930-club", "9:30 Club", "Washington", "DC", 1200, Club, nil, wiki + "9:30_Club"},
	{"hard-rock-live-orlando", "Hard Rock Live", "Orlando", "FL", 3000, Theater, nil, wiki + "Hard_Rock_Live_(Orlando)"},
	{"fillmore-miami-beach", "The Fillmore Miami Beach", "Miami Beach", "FL", 2700, Theater, nil, wiki + "Jackie_Gleason_Theater"},
	{"fox-theatre-atlanta", "Fox Theatre", "Atlanta", "GA", 4665, Theater, nil, wiki + "Fox_Theatre_(Atlanta)"},
	{"tabernacle-atlanta", "Tabernacle", "Atlanta", "GA", 2600, Ballroom, nil, wiki + "Tabernacle_(Atlanta)"},
	{"blaisdell-concert-hall", "Blaisdell Concert Hall", "Honolulu", "HI", 2158, Theater, nil, wiki + "Neal_S._Blaisdell_Center"},
	{"morrison-center", "Morrison Center", "Boise", "ID", 2000, Theater, nil, wiki + "Morrison_Center_for_the_Performing_Arts"},
	{"knitting-factory-boise", "Knitting Factory", "Boise", "ID", 1000, Club, nil, "https://bo.knittingfactory.com/"},
	{"chicago-theatre", "Chicago Theatre", "Chicago", "IL", 3600, Theater, []string{"Main Floor", "Mezzanine", "Balcony"}, wiki + "Chicago_Theatre"},
	{"aragon-ballroom", "Aragon Ballroom", "Chicago", "IL", 5000, Ballroom, nil, wiki + "Aragon_Ballroom"},
	{"riviera-theatre", "Riviera Theatre", "Chicago", "IL", 2500, Ballroom, nil, wiki + "Riviera_Theatre"},
	{"murat-theatre", "Murat Theatre", "Indianapolis", "IN", 2500, Theater, nil, wiki + "Old_National_Centre"},
	{"ruoff-music-center", "Ruoff Music Center", "Noblesville", "IN", 24000, Amphitheater, nil, wiki + "Ruoff_Music_Center"},
	{"hoyt-sherman-place", "Hoyt Sherman Place", "Des Moines", "IA", 1250, Theater, nil, wiki + "Hoyt_Sherman_Place"},
	{"wells-fargo-arena-dsm", "Wells Fargo Arena", "Des Moines", "IA", 16980, Arena, nil, wiki + "Wells_Fargo_Arena_(Iowa)"},
	{"azura-amphitheater", "Azura Amphitheater", "Bonner Springs", "KS", 18000, Amphitheater, nil, wiki + "Azura_Amphitheater"},
	{"liberty-hall-lawrence", "Liberty Hall", "Lawrence", "KS", 1000, Club, nil, wiki + "Liberty_Hall_(Lawrence,_Kansas)"},
	{"louisville-palace", "Louisville Palace", "Louisville", "KY", 2700, Theater, nil, wiki + "Louisville_Palace"},
	{"rupp-arena", "Rupp Arena", "Lexington", "KY", 20500, Arena, nil, wiki + "Rupp_Arena"},
	{"saenger-theatre-nola", "Saenger Theatre", "New Orleans", "LA", 2600, Theater, nil, wiki + "Saenger_Theatre_(New_Orleans)"},
	{"smoothie-king-center", "Smoothie King Center", "New Orleans", "LA", 17800, Arena, nil, wiki + "Smoothie_King_Center"},
	{"state-theatre-portland", "State Theatre", "Portland", "ME", 1870, Ballroom, nil, wiki + "State_Theatre_(Portland,_Maine)"},
	{"merriweather-post-pavilion", "Merriweather Post Pavilion", "Columbia", "MD", 19319, Amphitheater, nil, wiki + "Merriweather_Post_Pavilion"},
	{"the-lyric-baltimore", "The Lyric", "Baltimore", "MD", 2564, Theater, nil, wiki + "Lyric_Opera_House_(Baltimore)"},
	{"mgm-music-hall-fenway", "MGM Music Hall at Fenway", "Boston", "MA", 5009, Ballroom, nil, wiki + "MGM_Music_Hall_at_Fenway"},
	{"wang-theatre", "Wang Theatre", "Boston", "MA", 3500, Theater, nil, wiki + "Wang_Theatre"},
	{"house-of-blues-boston", "House of Blues", "Boston", "MA", 2425, Ballroom, nil, wiki + "House_of_Blues"},
	{"fox-theatre-detroit", "Fox Theatre", "Detroit", "MI", 5048, Theater, nil, wiki + "Fox_Theatre_(Detroit)"},
	{"the-fillmore-detroit", "The Fillmore Detroit", "Detroit", "MI", 2888, Theater, nil, wiki + "The_Fillmore_Detroit"},
	{"pine-knob", "Pine Knob Music Theatre", "Clarkston", "MI", 12500, Amphitheater, nil, wiki + "Pine_Knob_Music_Theatre"},
	{"first-avenue", "First Avenue", "Minneapolis", "MN", 1550, Club, nil, wiki + "First_Avenue_(nightclub)"},
	{"orpheum-minneapolis", "Orpheum Theatre", "Minneapolis", "MN", 2600, Theater, nil, wiki + "Orpheum_Theatre_(Minneapolis)"},
	{"thalia-mara-hall", "Thalia Mara Hall", "Jackson", "MS", 2000, Theater, nil, wiki + "Thalia_Mara_Hall"},
	{"the-pageant", "The Pageant", "St. Louis", "MO", 2300, Ballroom, nil, wiki + "The_Pageant"},
	{"starlight-theatre-kc", "Starlight Theatre", "Kansas City", "MO", 7958, Amphitheater, nil, wiki + "Starlight_Theatre_(Kansas_City,_Missouri)"},
	{"uptown-theater-kc", "Uptown Theater", "Kansas City", "MO", 1700, Ballroom, nil, wiki + "Uptown_Theater_(Kansas_City,_Missouri)"},
	{"the-wilma", "The Wilma", "Missoula", "MT", 1400, Theater, nil, wiki + "Wilma_Building"},
	{"kettlehouse-amphitheater", "KettleHouse Amphitheater", "Bonner", "MT", 4000, Amphitheater, nil, wiki + "KettleHouse_Amphitheater"},
	{"holland-center", "Holland Performing Arts Center", "Omaha", "NE", 2000, Theater, nil, wiki + "Holland_Performing_Arts_Center"},
	{"pinnacle-bank-arena", "Pinnacle Bank Arena", "Lincoln", "NE", 15500, Arena, nil, wiki + "Pinnacle_Bank_Arena"},
	{"the-colosseum", "The Colosseum at Caesars Palace", "Las Vegas", "NV", 4300, Theater, nil, wiki + "The_Colosseum_at_Caesars_Palace"},
	{"t-mobile-arena", "T-Mobile Arena", "Paradise", "NV", 20000, Arena, nil, wiki + "T-Mobile_Arena"},
	{"capitol-center-concord", "Capitol Center for the Arts", "Concord", "NH", 1300, Theater, nil, wiki + "Capitol_Center_for_the_Arts"},
	{"banknh-pavilion", "BankNH Pavilion", "Gilford", "NH", 9000, Amphitheater, nil, wiki + "Bank_of_New_Hampshire_Pavilion"},
	{"pnc-bank-arts-center", "PNC Bank Arts Center", "Holmdel", "NJ", 17500, Amphitheater, nil, wiki + "PNC_Bank_Arts_Center"},
	{"prudential-center", "Prudential Center", "Newark", "NJ", 18000, Arena, nil, wiki + "Prudential_Center"},
	{"the-stone-pony", "The Stone Pony", "Asbury Park", "NJ", 840, Club, nil, wiki + "The_Stone_Pony"},
	{"isleta-amphitheater", "Isleta Amphitheater", "Albuquerque", "NM", 12000, Amphitheater, nil, wiki + "Isleta_Amphitheater"},
	{"kiva-auditorium", "Kiva Auditorium", "Albuquerque", "NM", 2300, Theater, nil, wiki + "Albuquerque_Convention_Center"},
	{"radio-city", "Radio City Music Hall", "New York", "NY", 6000, Theater, []string{"Orchestra", "1st Mezzanine", "2nd Mezzanine", "3rd Mezzanine"}, wiki + "Radio_City_Music_Hall"},
	{"beacon-theatre", "Beacon Theatre", "New York", "NY", 2894, Theater, nil, wiki + "Beacon_Theatre_(New_York_City)"},
	{"brooklyn-steel", "Brooklyn Steel", "Brooklyn", "NY", 1800, Ballroom, nil, wiki + "Brooklyn_Steel"},
	{"madison-square-garden", "Madison Square Garden", "New York", "NY", 19500, Arena, nil, wiki + "Madison_Square_Garden"},
	{"red-hat-amphitheater", "Red Hat Amphitheater", "Raleigh", "NC", 5500, Amphitheater, nil, wiki + "Red_Hat_Amphitheater"},
	{"dpac", "DPAC", "Durham", "NC", 2712, Theater, nil, wiki + "Durham_Performing_Arts_Center"},
	{"the-ritz-raleigh", "The Ritz", "Raleigh", "NC", 1400, Club, nil, "https://www.ritzraleigh.com/"},
	{"fargo-theatre", "Fargo Theatre", "Fargo", "ND", 870, Theater, nil, wiki + "Fargo_Theatre"},
	{"ohio-theatre-columbus", "Ohio Theatre", "Columbus", "OH", 2800, Theater, nil, wiki + "Ohio_Theatre_(Columbus,_Ohio)"},
	{"riverbend-music-center", "Riverbend Music Center", "Cincinnati", "OH", 20500, Amphitheater, nil, wiki + "Riverbend_Music_Center"},
	{"house-of-blues-cleveland", "House of Blues", "Cleveland", "OH", 1200, Club, nil, wiki + "House_of_Blues"},
	{"cains-ballroom", "Cain's Ballroom", "Tulsa", "OK", 1700, Ballroom, nil, wiki + "Cain%27s_Ballroom"},
	{"the-criterion-okc", "The Criterion", "Oklahoma City", "OK", 4000, Ballroom, nil, "https://www.criterionokc.com/"},
	{"crystal-ballroom", "Crystal Ballroom", "Portland", "OR", 1500, Ballroom, nil, wiki + "Crystal_Ballroom"},
	{"arlene-schnitzer-concert-hall", "Arlene Schnitzer Concert Hall", "Portland", "OR", 2776, Theater, nil, wiki + "Arlene_Schnitzer_Concert_Hall"},
	{"moda-center", "Moda Center", "Portland", "OR", 19000, Arena, nil, wiki + "Moda_Center"},
	{"the-fillmore-philadelphia", "The Fillmore Philadelphia", "Philadelphia", "PA", 2500, Ballroom, nil, wiki + "The_Fillmore_Philadelphia"},
	{"stage-ae", "Stage AE", "Pittsburgh", "PA", 5500, Amphitheater, nil, wiki + "Stage_AE"},
	{"ppac", "Providence Performing Arts Center", "Providence", "RI", 3100, Theater, nil, wiki + "Providence_Performing_Arts_Center"},
	{"township-auditorium", "Township Auditorium", "Columbia", "SC", 3000, Theater, nil, wiki + "Township_Auditorium"},
	{"washington-pavilion", "Washington Pavilion", "Sioux Falls", "SD", 1800, Theater, nil, wiki + "Washington_Pavilion"},
	{"denny-sanford-premier-center", "Denny Sanford Premier Center", "Sioux Falls", "SD", 12000, Arena, nil, wiki + "Denny_Sanford_Premier_Center"},
	{"ryman", "Ryman Auditorium", "Nashville", "TN", 2362, Theater, []string{"Main Floor", "Balcony"}, wiki + "Ryman_Auditorium"},
	{"ascend-amphitheater", "Ascend Amphitheater", "Nashville", "TN", 6800, Amphitheater, nil, wiki + "Ascend_Amphitheater"},
	{"bridgestone-arena", "Bridgestone Arena", "Nashville", "TN", 20000, Arena, nil, wiki + "Bridgestone_Arena"},
	{"moody-center", "Moody Center", "Austin", "TX", 15000, Arena, nil, wiki + "Moody_Center"},
	{"acl-live", "ACL Live at the Moody Theater", "Austin", "TX", 2750, Theater, nil, wiki + "ACL_Live_at_the_Moody_Theater"},
	{"house-of-blues-houston", "House of Blues", "Houston", "TX", 1500, Club, nil, wiki + "House_of_Blues"},
	{"eccles-theater", "Eccles Theater", "Salt Lake City", "UT", 2500, Theater, nil, wiki + "Eccles_Theater"},
	{"usana-amphitheatre", "Utah First Credit Union Amphitheatre", "West Valley City", "UT", 20000, Amphitheater, nil, wiki + "USANA_Amphitheatre"},
	{"higher-ground", "Higher Ground", "South Burlington", "VT", 1000, Club, nil, wiki + "Higher_Ground_(music_venue)"},
	{"flynn-burlington", "The Flynn", "Burlington", "VT", 1400, Theater, nil, wiki + "Flynn_Center_for_the_Performing_Arts"},
	{"the-national-richmond", "The National", "Richmond", "VA", 1500, Ballroom, nil, wiki + "The_National_(Richmond,_Virginia)"},
	{"jiffy-lube-live", "Jiffy Lube Live", "Bristow", "VA", 25000, Amphitheater, nil, wiki + "Jiffy_Lube_Live"},
	{"the-norva", "The NorVa", "Norfolk", "VA", 1500, Club, nil, wiki + "The_NorVa"},
	{"paramount-theatre-seattle", "Paramount Theatre", "Seattle", "WA", 2807, Theater, nil, wiki + "Paramount_Theatre_(Seattle)"},
	{"the-showbox", "The Showbox", "Seattle", "WA", 1150, Club, nil, wiki + "The_Showbox"},
	{"climate-pledge-arena", "Climate Pledge Arena", "Seattle", "WA", 18100, Arena, nil, wiki + "Climate_Pledge_Arena"},
	{"gorge-amphitheatre", "The Gorge Amphitheatre", "George", "WA", 27500, Amphitheater, nil, wiki + "The_Gorge_Amphitheatre"},
	{"clay-center", "Clay Center", "Charleston", "WV", 1883, Theater, nil, wiki + "Clay_Center_(West_Virginia)"},
	{"riverside-theater-milwaukee", "Riverside Theater", "Milwaukee", "WI", 2450, Theater, nil, wiki + "Riverside_Theater_(Milwaukee)"},
	{"pabst-theater", "Pabst Theater", "Milwaukee", "WI", 1300, Theater, nil, wiki + "Pabst_Theater"},
	{"cheyenne-civic-center", "Cheyenne Civic Center", "Cheyenne", "WY", 1496, Theater, nil, "https://www.cheyennecity.org/Your-Government/Departments/Cheyenne-Civic-Center"},
}

// genericSections is each kind's seating, nearest the stage first.
var genericSections = map[Kind][]string{
	Club:         {"Floor", "Balcony"},
	Ballroom:     {"Floor", "Balcony"},
	Theater:      {"Orchestra", "Mezzanine", "Balcony"},
	Amphitheater: {"Pit", "Reserved", "Lawn"},
	Arena:        {"Floor", "Lower Bowl", "Upper Bowl"},
}

// priceRange is the typical ticket price range per kind, in cents. Anchored on Pollstar 2025:
// $34.74 average for clubs of 750 or fewer, $134.23 for North American top-100 tours, $216 stadiums.
var priceRange = map[Kind][2]int{
	Club:         {3000, 6500},
	Ballroom:     {4500, 9500},
	Theater:      {5500, 18500},
	Amphitheater: {6000, 22500},
	Arena:        {7500, 27500},
}

// SectionsOf returns the venue's real sections, or its kind's generic layout.
func SectionsOf(v Venue) []string {
	if len(v.Sections) > 0 {
		return v.Sections
	}
	return genericSections[v.Kind]
}

// StateByCode finds a state; ok is false for an unknown code.
func StateByCode(code string) (State, bool) {
	for _, s := range States {
		if s.Code == code {
			return s, true
		}
	}
	return State{}, false
}

// VenuesIn lists the venues in a state, in catalog order.
func VenuesIn(code string) []Venue {
	var out []Venue
	for _, v := range Venues {
		if v.State == code {
			out = append(out, v)
		}
	}
	return out
}

// VenueByKey finds a venue by key.
func VenueByKey(key string) (Venue, bool) {
	for _, v := range Venues {
		if v.Key == key {
			return v, true
		}
	}
	return Venue{}, false
}
