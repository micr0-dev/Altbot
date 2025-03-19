/*
 * Copyright (C) 2025 Micr0Byte <micr0@micr0.dev>
 * Licensed under the Overworked License (OWL) v2.0
 */

let charts = {};

async function fetchMetrics() {
    const response = await fetch('/api/metrics');
    const data = await response.json();
    return data;
}

// Add this helper function at the top of your charts.js
function isDarkMode() {
    return window.matchMedia && window.matchMedia('(prefers-color-scheme: dark)').matches;
}

// Create a chart options helper
function getChartDefaults() {
    const textColor = isDarkMode() ? '#f1f5f9' : '#1e293b';
    return {
        color: textColor,
        plugins: {
            legend: {
                labels: {
                    color: textColor
                }
            }
        },
        responsive: true,
        maintainAspectRatio: false,
        aspectRatio: 1,
    };
}

function processMetrics(data) {
    const eventCounts = {};
    const mediaTypeCounts = {};
    const languageCounts = {};  // Add language counts
    const userSet = new Set();
    const imageResponseTimes = [];
    const hourlyActivity = Array(24).fill(0);

    data.forEach(event => {
        // Count events
        eventCounts[event.EventType] = (eventCounts[event.EventType] || 0) + 1;

        // Collect unique users
        userSet.add(event.UserID);

        // Process successful generations
        if (event.EventType === 'successful_generation' && event.Details) {
            // Count media types
            const mediaType = event.Details.mediaType;
            mediaTypeCounts[mediaType] = (mediaTypeCounts[mediaType] || 0) + 1;

            // Count languages
            if (event.Details.lang) {
                // Convert language code to full name
                const lang = getLanguageName(event.Details.lang);
                languageCounts[lang] = (languageCounts[lang] || 0) + 1;
            }

            if (mediaType === 'image' && event.Details.responseTime) {
                imageResponseTimes.push({
                    timestamp: new Date(event.Timestamp),
                    responseTime: event.Details.responseTime
                });
            }
        }

        // Hourly activity
        const hour = new Date(event.Timestamp).getHours();
        hourlyActivity[hour]++;
    });

    // Calculate average response time
    const avgResponseTime = imageResponseTimes.length > 0
        ? Math.round(imageResponseTimes.reduce((acc, curr) => acc + curr.responseTime, 0) / imageResponseTimes.length)
        : 0;

    const userEngagement = {
        dates: {},
        newUsers: {},
        activeUsers: {}
    };

    data.forEach(event => {
        const date = new Date(event.Timestamp).toISOString().split('T')[0];
        if (!userEngagement.dates[date]) {
            userEngagement.dates[date] = new Set();
            userEngagement.newUsers[date] = new Set();
        }
        userEngagement.dates[date].add(event.UserID);

        // Check if this is user's first activity
        const userFirstActivity = data
            .find(e => e.UserID === event.UserID)
            .Timestamp.split('T')[0];
        if (userFirstActivity === date) {
            userEngagement.newUsers[date].add(event.UserID);
        }
    });

    // Convert Sets to counts
    Object.keys(userEngagement.dates).forEach(date => {
        userEngagement.activeUsers[date] = userEngagement.dates[date].size;
        userEngagement.newUsers[date] = userEngagement.newUsers[date].size;
    });

    return {
        eventCounts,
        mediaTypeCounts,
        languageCounts, 
        uniqueUsers: userSet.size,
        imageResponseTimes,
        hourlyActivity,
        avgResponseTime,
        userEngagement
    };
}

// Helper function to convert language codes to names
function getLanguageName(code) {
    const languages = {
        'en': 'English',
        'it': 'Italian',
        'es': 'Spanish',
        'fr': 'French',
        'de': 'German',
        'pt': 'Portuguese',
        'ru': 'Russian',
        'ja': 'Japanese',
        'zh': 'Chinese',
        'ko': 'Korean',
        'ar': 'Arabic',
        'hi': 'Hindi',
        'tr': 'Turkish',
        'nl': 'Dutch',
        'pl': 'Polish',
        'sv': 'Swedish',
        'da': 'Danish',
        'fi': 'Finnish',
        'no': 'Norwegian',
        'uk': 'Ukrainian',
        'cs': 'Czech',
        'hu': 'Hungarian',
        'ro': 'Romanian',
        'el': 'Greek',
        'th': 'Thai',
        'vi': 'Vietnamese',
    };

    return languages[code] || code; // Return the code if no mapping exists
}

function updateCharts(metrics) {
    const defaultOptions = getChartDefaults();

    // Event Distribution Pie Chart
    const ctx1 = document.getElementById('eventsPie').getContext('2d');
    if (charts.eventsPie) charts.eventsPie.destroy();
    charts.eventsPie = new Chart(ctx1, {
        type: 'pie',
        data: {
            labels: Object.keys(metrics.eventCounts),
            datasets: [{
                data: Object.values(metrics.eventCounts),
                backgroundColor: [
                    '#6366f1',
                    '#8b5cf6',
                    '#d946ef',
                    '#ec4899',
                    '#f43f5e'
                ]
            }]
        },
        options: {
            ...defaultOptions,
            plugins: {
                legend: {
                    display: false
                }
            }
        }
    });

    // Media Type Distribution Pie Chart
    const ctx2 = document.getElementById('languagePie').getContext('2d');
    if (charts.languagePie) charts.languagePie.destroy();

    // Get top languages (limit to top 8 for readability)
    const sortedLanguages = Object.entries(metrics.languageCounts)
        .sort((a, b) => b[1] - a[1]);

    // If we have more than 8 languages, group the rest as "Other"
    let languageLabels = [];
    let languageData = [];

    if (sortedLanguages.length > 8) {
        languageLabels = sortedLanguages.slice(0, 7).map(item => item[0]);
        languageData = sortedLanguages.slice(0, 7).map(item => item[1]);

        // Add "Other" category
        const otherCount = sortedLanguages.slice(7).reduce((acc, curr) => acc + curr[1], 0);
        languageLabels.push('Other');
        languageData.push(otherCount);
    } else {
        languageLabels = sortedLanguages.map(item => item[0]);
        languageData = sortedLanguages.map(item => item[1]);
    }

    charts.languagePie = new Chart(ctx2, {
        type: 'pie',
        data: {
            labels: languageLabels,
            datasets: [{
                data: languageData,
                backgroundColor: [
                    '#6366f1', '#8b5cf6', '#d946ef', '#ec4899', 
                    '#f43f5e', '#f97316', '#eab308', '#84cc16'
                ]
            }]
        },
        options: {
            ...defaultOptions,
            plugins: {
                legend: {
                    display: false,
                    position: 'right'
                },
                tooltip: {
                    callbacks: {
                        label: function(context) {
                            const label = context.label || '';
                            const value = context.raw || 0;
                            const total = context.dataset.data.reduce((a, b) => a + b, 0);
                            const percentage = Math.round((value / total) * 100);
                            return `${label}: ${value} (${percentage}%)`;
                        }
                    }
                }
            }
        }
    });

    const ctx3 = document.getElementById('combinedChart').getContext('2d');
    if (charts.combinedChart) charts.combinedChart.destroy();

    // Calculate average response times per hour
    const hourlyResponseTimes = Array(24).fill(0).map(() => ({ sum: 0, count: 0 }));
    metrics.imageResponseTimes.forEach(item => {
        const hour = new Date(item.timestamp).getHours();
        hourlyResponseTimes[hour].sum += item.responseTime;
        hourlyResponseTimes[hour].count++;
    });

    const avgResponseTimes = hourlyResponseTimes.map(h =>
        h.count > 0 ? h.sum / h.count : 0);

    // Get current hour for the line
    const currentHour = new Date().getHours();

    charts.combinedChart = new Chart(ctx3, {
        type: 'line',
        data: {
            labels: Array.from({ length: 24 }, (_, i) => `${i}:00`),
            datasets: [{
                label: 'Activity',
                data: metrics.hourlyActivity,
                type: 'bar',
                backgroundColor: 'rgba(99, 102, 241, 0.5)',
                borderRadius: 4,
                yAxisID: 'y1'
            }, {
                label: 'Response Time (ms)',
                data: avgResponseTimes,
                borderColor: '#ef4444',
                tension: 0.4,
                fill: false,
                yAxisID: 'y2'
            }]
        },
        options: {
            ...defaultOptions,
            scales: {
                y1: {
                    type: 'linear',
                    position: 'left',
                    grid: {
                        display: false
                    },
                    ticks: {
                        color: defaultOptions.color
                    }
                },
                y2: {
                    type: 'linear',
                    position: 'right',
                    grid: {
                        display: false
                    },
                    ticks: {
                        color: defaultOptions.color
                    }
                },
                x: {
                    grid: {
                        display: false
                    },
                    ticks: {
                        color: defaultOptions.color
                    }
                }
            },
            plugins: {
                annotation: {
                    annotations: {
                        currentTime: {
                            type: 'line',
                            xMin: currentHour,
                            xMax: currentHour,
                            borderColor: isDarkMode() ? 'rgba(255, 255, 255, 0.5)' : 'rgba(0, 0, 0, 0.5)',
                            borderWidth: 2,
                            borderDash: [5, 5],
                            label: {
                                display: true,
                                content: 'Now',
                                position: 'top'
                            }
                        }
                    }
                }
            }
        }
    });

    // Update stats
    document.getElementById('totalRequests').textContent =
        Object.values(metrics.eventCounts).reduce((a, b) => a + b, 0);
    document.getElementById('totalUsers').textContent = metrics.uniqueUsers;
    document.getElementById('avgResponseTime').textContent = `${metrics.avgResponseTime}ms`;
    document.getElementById('rateLimits').textContent =
        metrics.eventCounts.rate_limit_hit || 0;
}

// Add dark mode listener
window.matchMedia('(prefers-color-scheme: dark)').addEventListener('change', () => {
    updateDashboard();
});

async function updateDashboard() {
    const metrics = await fetchMetrics();
    const processed = processMetrics(metrics);
    updateCharts(processed);
}

document.addEventListener('DOMContentLoaded', async () => {
    // Initial load of both charts and timeline
    const metrics = await fetchMetrics();
    const processed = processMetrics(metrics);
    updateCharts(processed);
    updateTimeline(metrics);

    setInterval(updateDashboard, 60000);
});

function formatEventType(type) {
    return type
        .split('_')
        .map(word => word.charAt(0).toUpperCase() + word.slice(1))
        .join(' ');
}

function getEventIcon(type) {
    const icons = {
        request: '🔍',
        successful_generation: '✨',
        rate_limit_hit: '⚠️',
        follow: '👤',
        error: '❌'
    };
    return icons[type] || '📝';
}

function getEventColor(type) {
    const colors = {
        request: 'linear-gradient(135deg, #6366f1, #8b5cf6)',
        successful_generation: 'linear-gradient(135deg, #22c55e, #16a34a)',
        rate_limit_hit: 'linear-gradient(135deg, #f59e0b, #d97706)',
        follow: 'linear-gradient(135deg, #06b6d4, #0891b2)',
        error: 'linear-gradient(135deg, #ef4444, #dc2626)'
    };
    return colors[type] || 'linear-gradient(135deg, #6366f1, #8b5cf6)';
}

function formatTimeAgo(timestamp) {
    const seconds = Math.floor((new Date() - new Date(timestamp)) / 1000);

    const intervals = {
        year: 31536000,
        month: 2592000,
        week: 604800,
        day: 86400,
        hour: 3600,
        minute: 60,
        second: 1
    };

    for (const [unit, secondsInUnit] of Object.entries(intervals)) {
        const interval = Math.floor(seconds / secondsInUnit);
        if (interval >= 1) {
            return `${interval} ${unit}${interval === 1 ? '' : 's'} ago`;
        }
    }
    return 'just now';
}

let currentPage = 1;
const eventsPerPage = 20;
let allEvents = []; // Store all events

function updateTimeline(data, append = false) {
    const timeline = document.getElementById('timeline');
    const loadMoreBtn = document.getElementById('loadMoreBtn');

    // Store all events on first load
    if (!append) {
        allEvents = [...data].sort((a, b) =>
            new Date(b.Timestamp) - new Date(a.Timestamp));
        timeline.innerHTML = ''; // Clear only on first load
        currentPage = 1; // Reset page count on fresh load
    }

    // Calculate the correct slice of events
    const startIndex = (currentPage - 1) * eventsPerPage;
    const endIndex = currentPage * eventsPerPage;
    const eventsToShow = allEvents.slice(startIndex, endIndex);

    // Add events to timeline
    eventsToShow.forEach(event => {
        const timeAgo = formatTimeAgo(event.Timestamp);
        const formattedType = formatEventType(event.EventType);

        let details = '';
        if (event.Details) {
            if (event.Details.mediaType) {
                details += `Media Type: ${event.Details.mediaType}`;
            }
            if (event.Details.responseTime) {
                details += details ? ' • ' : '';
                details += `Response Time: ${event.Details.responseTime}ms`;
            }
        }

        const itemHTML = `
            <div class="timeline-item">
                <div class="timeline-content">
                    <div class="timeline-time">${timeAgo}</div>
                    <div class="timeline-title">
                        ${getEventIcon(event.EventType)} ${formattedType}
                    </div>
                    ${details ? `<div class="timeline-details">${details}</div>` : ''}
                    <div class="timeline-tag" style="background: ${getEventColor(event.EventType)}">
                        User ID: ${event.UserID.slice(0, 8)}...
                    </div>
                </div>
            </div>
        `;

        timeline.insertAdjacentHTML('beforeend', itemHTML);
    });

    // Update button state
    loadMoreBtn.disabled = endIndex >= allEvents.length;
    if (loadMoreBtn.disabled) {
        loadMoreBtn.textContent = 'No More Events';
    } else {
        loadMoreBtn.textContent = 'Load More';
    }
}

// Add event listener for the Load More button
document.getElementById('loadMoreBtn').addEventListener('click', () => {
    currentPage++;
    updateTimeline(null, true);
});

async function initializeTimeline() {
    const metrics = await fetchMetrics();
    updateTimeline(metrics);
}
