package weather

// Describe maps a WMO weather code, as Open-Meteo reports it, to a short
// English description and an emoji. Unknown codes give a neutral fallback
// rather than an error, since a summary is better with vague weather than none.
func Describe(code int) (text, emoji string) {
	switch code {
	case 0:
		return "Clear sky", "☀️"
	case 1:
		return "Mostly clear", "🌤️"
	case 2:
		return "Partly cloudy", "⛅"
	case 3:
		return "Overcast", "☁️"
	case 45, 48:
		return "Fog", "🌫️"
	case 51, 53, 55:
		return "Drizzle", "🌦️"
	case 56, 57:
		return "Freezing drizzle", "🌧️"
	case 61, 63, 65:
		return "Rain", "🌧️"
	case 66, 67:
		return "Freezing rain", "🌧️"
	case 71, 73, 75, 77:
		return "Snow", "❄️"
	case 80, 81, 82:
		return "Rain showers", "🌦️"
	case 85, 86:
		return "Snow showers", "🌨️"
	case 95, 96, 99:
		return "Thunderstorm", "⛈️"
	}
	return "Unsettled", "🌡️"
}
