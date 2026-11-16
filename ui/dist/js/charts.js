/* ========================================================
   NEXDNS — REAL-TIME SVG TRAFFIC CHART (ZERO DEPENDENCY)
   ======================================================== */

class TrafficChart {
  constructor(svgId) {
    this.svg = document.getElementById(svgId);
    if (!this.svg) return;
    this.maxPoints = 60;
    this.downData = new Array(this.maxPoints).fill(0);
    this.upData = new Array(this.maxPoints).fill(0);
    this.resize();
    window.addEventListener('resize', () => this.resize());
  }

  resize() {
    if (!this.svg) return;
    const rect = this.svg.getBoundingClientRect();
    this.width = Math.max(rect.width, 1);
    this.height = Math.max(rect.height, 1);
    this.svg.setAttribute('viewBox', `0 0 ${this.width} ${this.height}`);
    this.draw();
  }

  addData(downBps, upBps) {
    this.downData.push(downBps);
    if (this.downData.length > this.maxPoints) this.downData.shift();
    this.upData.push(upBps);
    if (this.upData.length > this.maxPoints) this.upData.shift();
    this.draw();
  }

  draw() {
    if (!this.svg) return;
    const width = this.width || 1;
    const height = this.height || 1;
    let maxVal = Math.max(...this.downData, ...this.upData, 1024 * 50);
    maxVal *= 1.15;

    const style = getComputedStyle(document.body);
    const colorDown = style.getPropertyValue('--chart-line-down').trim() || '#8b5cf6';
    const colorUp = style.getPropertyValue('--chart-line-up').trim() || '#06b6d4';
    const colorGrid = style.getPropertyValue('--chart-grid').trim() || style.getPropertyValue('--border').trim() || '#2b2b42';
    const defs = document.createElementNS('http://www.w3.org/2000/svg', 'defs');
    const gradient = document.createElementNS('http://www.w3.org/2000/svg', 'linearGradient');
    const gradientId = `${this.svg.id}-download-fill`;
    gradient.setAttribute('id', gradientId);
    gradient.setAttribute('x1', '0');
    gradient.setAttribute('y1', '0');
    gradient.setAttribute('x2', '0');
    gradient.setAttribute('y2', '1');

    const start = document.createElementNS('http://www.w3.org/2000/svg', 'stop');
    start.setAttribute('offset', '0%');
    start.setAttribute('stop-color', colorDown);
    start.setAttribute('stop-opacity', '0.2');
    const end = document.createElementNS('http://www.w3.org/2000/svg', 'stop');
    end.setAttribute('offset', '100%');
    end.setAttribute('stop-color', colorDown);
    end.setAttribute('stop-opacity', '0');
    gradient.append(start, end);
    defs.append(gradient);

    const elements = [defs];
    for (let i = 1; i < 4; i++) {
      const y = (height / 4) * i;
      const line = document.createElementNS('http://www.w3.org/2000/svg', 'line');
      line.setAttribute('x1', '0');
      line.setAttribute('x2', String(width));
      line.setAttribute('y1', String(y));
      line.setAttribute('y2', String(y));
      line.setAttribute('stroke', colorGrid);
      line.setAttribute('stroke-width', '1');
      line.setAttribute('stroke-dasharray', '4 4');
      elements.push(line);
    }

    const downloadPath = this.linePath(this.downData, maxVal, width, height);
    const uploadPath = this.linePath(this.upData, maxVal, width, height);
    const area = document.createElementNS('http://www.w3.org/2000/svg', 'path');
    area.setAttribute('d', `${downloadPath} L ${width} ${height} L 0 ${height} Z`);
    area.setAttribute('fill', `url(#${gradientId})`);
    elements.push(area);
    elements.push(this.createLine(downloadPath, colorDown));
    elements.push(this.createLine(uploadPath, colorUp));
    this.svg.replaceChildren(...elements);
  }

  linePath(data, maxVal, width, height) {
    const step = width / (this.maxPoints - 1);
    return data.map((value, index) => {
      const x = index * step;
      const y = height - (value / maxVal) * (height - 20) - 10;
      return `${index === 0 ? 'M' : 'L'} ${x} ${y}`;
    }).join(' ');
  }

  createLine(path, color) {
    const line = document.createElementNS('http://www.w3.org/2000/svg', 'path');
    line.setAttribute('d', path);
    line.setAttribute('fill', 'none');
    line.setAttribute('stroke', color);
    line.setAttribute('stroke-width', '2.5');
    line.setAttribute('stroke-linecap', 'round');
    line.setAttribute('stroke-linejoin', 'round');
    return line;
  }
}
