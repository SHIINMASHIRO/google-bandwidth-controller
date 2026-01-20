// Dashboard Application
class Dashboard {
    constructor() {
        this.apiBase = '';
        this.refreshInterval = 2000;
        this.chart = null;
        this.historyData = [];
        this.maxHistoryPoints = 60;

        this.init();
    }

    async init() {
        this.initChart();
        document.getElementById('api-host').textContent = window.location.host;
        await this.fetchAndUpdate();
        setInterval(() => this.fetchAndUpdate(), this.refreshInterval);
    }

    initChart() {
        const ctx = document.getElementById('bandwidth-chart').getContext('2d');
        this.chart = new Chart(ctx, {
            type: 'line',
            data: {
                labels: [],
                datasets: [
                    {
                        label: 'Current (Gbps)',
                        data: [],
                        borderColor: '#3b82f6',
                        backgroundColor: 'rgba(59, 130, 246, 0.1)',
                        fill: true,
                        tension: 0.4,
                        pointRadius: 0
                    },
                    {
                        label: 'Target',
                        data: [],
                        borderColor: '#ef4444',
                        borderDash: [5, 5],
                        pointRadius: 0,
                        fill: false
                    }
                ]
            },
            options: {
                responsive: true,
                maintainAspectRatio: false,
                interaction: {
                    intersect: false,
                    mode: 'index'
                },
                scales: {
                    y: {
                        beginAtZero: true,
                        grid: { color: 'rgba(255,255,255,0.05)' },
                        ticks: { color: '#6b7280', font: { size: 10 } }
                    },
                    x: {
                        grid: { display: false },
                        ticks: { color: '#6b7280', font: { size: 10 }, maxTicksLimit: 8 }
                    }
                },
                plugins: {
                    legend: {
                        display: true,
                        position: 'top',
                        align: 'end',
                        labels: { color: '#9ca3af', boxWidth: 12, font: { size: 10 } }
                    }
                }
            }
        });
    }

    async fetchAndUpdate() {
        try {
            const [metrics, status, agents] = await Promise.all([
                this.fetchJSON('/metrics'),
                this.fetchJSON('/status'),
                this.fetchJSON('/agents')
            ]);

            this.updateStatus(metrics, status);
            this.updateChart(metrics);
            this.updateAgents(agents);
            this.updateConnectionStatus(true);
            this.updateLastRefresh();

        } catch (error) {
            console.error('Failed to fetch data:', error);
            this.updateConnectionStatus(false);
        }
    }

    async fetchJSON(endpoint) {
        const response = await fetch(this.apiBase + endpoint);
        if (!response.ok) throw new Error(`HTTP ${response.status}`);
        return response.json();
    }

    updateStatus(metrics, status) {
        const currentGbps = (metrics.total_bandwidth_gbps || 0).toFixed(2);
        const targetGbps = (metrics.target_bandwidth_gbps || 0).toFixed(1);
        const percentage = (metrics.target_percentage || 0).toFixed(1);

        // Update gauge
        const gaugeEl = document.getElementById('bandwidth-gauge');
        gaugeEl.querySelector('.text-4xl').textContent = currentGbps;
        gaugeEl.querySelector('.text-gray-500').textContent = `of ${targetGbps} Gbps`;

        const bar = document.getElementById('bandwidth-bar');
        bar.style.width = `${Math.min(percentage, 100)}%`;
        if (percentage >= 90) {
            bar.className = 'h-full bg-green-500 transition-all duration-500';
        } else if (percentage >= 70) {
            bar.className = 'h-full bg-yellow-500 transition-all duration-500';
        } else {
            bar.className = 'h-full bg-blue-500 transition-all duration-500';
        }

        document.getElementById('bandwidth-percent').textContent = `${percentage}%`;

        // Update status fields
        const phase = status.phase || 'unknown';
        const phaseEl = document.getElementById('phase');
        phaseEl.textContent = phase.toUpperCase();
        phaseEl.className = 'font-medium';
        if (phase === 'stable') phaseEl.classList.add('text-green-400');
        else if (phase === 'ramping_up') phaseEl.classList.add('text-yellow-400');
        else if (phase === 'ramping_down') phaseEl.classList.add('text-orange-400');
        else phaseEl.classList.add('text-gray-400');

        document.getElementById('next-rotation').textContent = status.time_until_rotation || '--';
        document.getElementById('active-agents').textContent =
            `${status.active_agents || 0}/${metrics.total_agents || 0}`;
        document.getElementById('rotation-count').textContent = status.rotation_count || '0';
    }

    updateChart(metrics) {
        const now = new Date().toLocaleTimeString('en-US', { hour12: false, hour: '2-digit', minute: '2-digit', second: '2-digit' });
        const currentGbps = metrics.total_bandwidth_gbps || 0;
        const targetGbps = metrics.target_bandwidth_gbps || 0;

        this.historyData.push({ time: now, current: currentGbps, target: targetGbps });

        if (this.historyData.length > this.maxHistoryPoints) {
            this.historyData.shift();
        }

        this.chart.data.labels = this.historyData.map(d => d.time);
        this.chart.data.datasets[0].data = this.historyData.map(d => d.current);
        this.chart.data.datasets[1].data = this.historyData.map(d => d.target);
        this.chart.update('none');
    }

    updateAgents(agentsData) {
        const container = document.getElementById('agents-list');
        const agents = agentsData.agents || [];

        // Sort by agent name numerically (VPS-HK-1, VPS-HK-2, ... VPS-HK-10, VPS-HK-11)
        agents.sort((a, b) => {
            const numA = parseInt(a.name.match(/\d+$/)?.[0] || '0');
            const numB = parseInt(b.name.match(/\d+$/)?.[0] || '0');
            return numA - numB;
        });

        if (agents.length === 0) {
            container.innerHTML = '<div class="text-gray-500 text-sm">No agents configured</div>';
            return;
        }

        container.innerHTML = agents.map(agent => {
            const maxBw = agent.max_bandwidth || 1000;
            const percentage = Math.min((agent.current_bandwidth / maxBw) * 100, 100);

            let statusClass, statusIcon;
            if (!agent.connected) {
                statusClass = 'text-red-500';
                statusIcon = '\u2717';
            } else if (agent.current_bandwidth > 0) {
                statusClass = 'text-green-500';
                statusIcon = '\u2713';
            } else {
                statusClass = 'text-gray-500';
                statusIcon = '\u25CB';
            }

            const name = agent.name || agent.id;
            const region = agent.region ? `<span class="text-gray-600 text-xs ml-1">(${agent.region})</span>` : '';

            return `
                <div class="flex items-center gap-3 py-2 px-3 bg-gray-700/30 rounded text-sm">
                    <div class="w-32 truncate font-medium">${name}${region}</div>
                    <div class="flex-1">
                        <div class="bg-gray-700 rounded-full h-2 overflow-hidden">
                            <div class="h-full bg-blue-500 transition-all duration-300" style="width: ${percentage}%"></div>
                        </div>
                    </div>
                    <div class="w-20 text-right font-mono text-gray-400">${Math.round(agent.current_bandwidth)} Mbps</div>
                    <div class="w-5 text-center ${statusClass}">${statusIcon}</div>
                </div>
            `;
        }).join('');
    }

    updateConnectionStatus(connected) {
        const el = document.getElementById('connection-status');
        if (connected) {
            el.innerHTML = `
                <span class="status-dot status-active"></span>
                <span class="text-green-400">Connected</span>
            `;
        } else {
            el.innerHTML = `
                <span class="status-dot status-disconnected"></span>
                <span class="text-red-400">Disconnected</span>
            `;
        }
    }

    updateLastRefresh() {
        const now = new Date().toLocaleTimeString('en-US', { hour12: false });
        document.getElementById('last-update').textContent = now;
    }
}

document.addEventListener('DOMContentLoaded', () => new Dashboard());
