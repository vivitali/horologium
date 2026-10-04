# Changelog

All notable changes to the Winnipeg Tech Events Scraper & Telegram Sharing Web App will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added
- **Meetup bot** (`cmd/meetup`, Lambda `winnipeg-meetup-bot`): a dedicated Telegram bot that polls the group about the last weekend of each month (Fri evening / Sat noon / Sun noon / an AI-generated idea from Claude Haiku / can't make it), reads the counts back with `stopPoll` three days before the weekend, and posts + pins the winner when it has at least 4 votes. State lives in SSM `/winnipeg-tech-events/meetup-state`; a daily EventBridge tick drives it. New env: `TELEGRAM_MEETUP_BOT_TOKEN`, `TELEGRAM_MEETUP_CHAT_ID`, `ANTHROPIC_API_KEY`, `MEETUP_SSM_PARAM`.
- `scripts/check`, `scripts/test`, `scripts/build`, `scripts/ship` contract scripts and a repo `CLAUDE.md`.

### Removed
- The day-of-week poll on the 20th (`poll` action, `winnipeg-poll-monthly` rule, `TELEGRAM_POLL_*` env vars). Absorbed by the meetup bot; `deploy.sh` deletes the old rule.

### Changed
- **Deploys are manual.** The deploy workflow no longer runs on push to `main` and no longer invokes the events digest as a smoke test, so merging and deploying never post to the group.
- `internal/ssmparam` extracted from `internal/annual` so both Lambdas share one SSM adapter.
- **Annual events footer is now scraped monthly** instead of read from a hardcoded slice. A new Lambda action `scrape_annual` runs on the 1st of each month via EventBridge, scrapes four Winnipeg annual-event sites (Prairie Dev Con, WomenHack, North Forge RampUp, MbTech Week), and persists the next-occurrence dates to SSM Parameter Store. The weekly digest reads the latest result from SSM at send time. Per-site scrape failures retain last-known-good data, so a single broken parser never empties the footer.

## [1.0.0] - 2025-01-13

### Added
- **Multi-source event aggregation** from Meetup.com, Eventbrite, and Dev.events
- **Smart date parsing** with support for day names ("Thu", "Saturday") and ISO datetime formats
- **Telegram integration** with Bot API and share URL functionality
- **Modern responsive web UI** with dark mode support and mobile optimization
- **Robust error handling** with automatic fallback to sample data
- **Real-time debug console** with comprehensive logging and export functionality
- **Event filtering and grouping** by date range, source, and search terms
- **Character count warnings** for Telegram message limits (4096 characters)
- **Production-ready architecture** with graceful degradation and fault tolerance

### Technical Features
- **Go backend** with HTTP server and REST API endpoints
- **JavaScript frontend** with modern ES6+ features
- **CORS support** for cross-origin requests
- **Modular scraper architecture** for easy addition of new event sources
- **Duplicate event removal** based on URL and name similarity
- **Chronological event sorting** with timezone-aware date handling
- **Comprehensive sample dataset** for offline mode and testing

### Error Handling & Reliability
- **Automatic fallback** to sample data when scraping fails
- **Individual source failure isolation** - one source failure doesn't break others
- **Network timeout handling** with 30-second limits
- **Parse error recovery** - malformed events are logged and skipped
- **UI error alerts** with dismissible banners and retry functionality
- **Debug logging** with timestamped entries and severity levels

### User Experience
- **Loading states** with spinners and progress indicators
- **Event selection** with checkboxes for Telegram sharing
- **Message preview** with real-time character counting
- **Responsive design** that works on desktop, tablet, and mobile
- **Accessibility features** with proper ARIA labels and keyboard navigation
- **Dark mode support** that adapts to system preferences

### Data Sources
- **Meetup.com**: Winnipeg tech events with smart day name parsing
- **Eventbrite**: Tech events with ISO datetime parsing and timezone handling
- **Dev.events**: Developer events in Winnipeg/Manitoba with date range support

### Telegram Features
- **Bot API integration** for direct messaging to configured groups
- **Share URL generation** for manual sharing in any chat
- **Message formatting** with Markdown support and event details
- **Character limit monitoring** with warnings at 3500+ characters
- **Event selection interface** with select all/clear options

### Development & Testing
- **Comprehensive README** with setup, configuration, and troubleshooting guides
- **Manual testing checklist** for validating all features and error scenarios
- **Load testing instructions** with hey tool integration
- **Docker deployment** configuration for containerized deployment
- **Environment variable** configuration for production settings

### Security & Performance
- **CORS headers** properly configured for cross-origin requests
- **Input sanitization** to prevent XSS attacks
- **Rate limiting considerations** for external API calls
- **Memory-efficient** event processing and rendering
- **Optimized CSS** with modern features and minimal bundle size

### Documentation
- **Detailed README** with architecture overview and API documentation
- **Code comments** throughout the codebase for maintainability
- **Error message documentation** for troubleshooting common issues
- **Deployment guides** for various environments
- **Contributing guidelines** for future development

## Validation Instructions

### Manual Testing Checklist

#### Basic Functionality
- [ ] Application loads at `http://localhost:8080`
- [ ] Events display in grouped format (Today, This Week, Next Week, Later)
- [ ] Date filters work correctly
- [ ] Source filters work correctly
- [ ] Search functionality works
- [ ] Event selection works for Telegram sharing

#### Error Handling
- [ ] Network failure shows warning banner
- [ ] Fallback to sample data works automatically
- [ ] Debug console shows detailed logs
- [ ] "Try Again" button resets state and retries
- [ ] Individual source failures don't break other sources

#### Telegram Integration
- [ ] Message preview updates when selecting events
- [ ] Character count shows current usage
- [ ] Character warning appears at 3500+ characters
- [ ] Share via URL opens Telegram with pre-filled message
- [ ] Bot API works with valid credentials (if configured)

#### Date Handling
- [ ] Events are sorted chronologically
- [ ] "Today" filter shows only today's events
- [ ] "This Week" filter shows current week events
- [ ] "Next Week" filter shows upcoming week events
- [ ] Date parsing handles various formats correctly

#### UI/UX
- [ ] Responsive design works on mobile devices
- [ ] Dark mode adapts to system preference
- [ ] Loading states show during data fetch
- [ ] Error banners are dismissible
- [ ] All buttons and controls are accessible

### Automated Testing

```bash
# Run Go tests
go test ./...

# Run load tests
go install github.com/rakyll/hey@latest
hey -n 100 -c 10 http://localhost:8080/api/events
```

### Error Scenario Testing

1. **Network Disconnection**: Disconnect internet and verify fallback to sample data
2. **Invalid Bot Token**: Enter invalid Telegram bot token and verify error handling
3. **Large Message**: Select many events to exceed character limit and verify warnings
4. **Empty Results**: Test with date filters that return no results
5. **JavaScript Errors**: Disable JavaScript and verify graceful degradation

### Performance Testing

1. **Load Testing**: Use hey tool to test with 100+ concurrent requests
2. **Memory Usage**: Monitor memory consumption during extended use
3. **Response Times**: Verify API responses under 500ms for typical queries
4. **Browser Compatibility**: Test in Chrome, Firefox, Safari, and Edge

### Security Testing

1. **XSS Prevention**: Test with malicious input in search fields
2. **CORS Configuration**: Verify proper CORS headers for cross-origin requests
3. **Input Validation**: Test with various malformed inputs
4. **Rate Limiting**: Verify graceful handling of rapid requests

## Future Enhancements

### Planned Features
- [ ] **Real-time scraping** with WebSocket updates
- [ ] **Event caching** with Redis for improved performance
- [ ] **User authentication** for personalized event tracking
- [ ] **Event favoriting** and personal calendars
- [ ] **Email notifications** for upcoming events
- [ ] **CSV/JSON export** functionality
- [ ] **Advanced filtering** by price, venue, and event type
- [ ] **Event analytics** and attendance tracking

### Technical Improvements
- [ ] **Web scraping optimization** with headless browser support
- [ ] **Database integration** for persistent event storage
- [ ] **API rate limiting** and caching strategies
- [ ] **Monitoring and alerting** for production deployments
- [ ] **Automated testing** with comprehensive test suites
- [ ] **Performance optimization** with lazy loading and virtualization
