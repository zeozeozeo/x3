let currentConfig = null;
let editingModelIndex = -1;

// Initialize the application
document.addEventListener('DOMContentLoaded', function() {
    loadConfig();
    setupEventListeners();
    setupTabs();
});

function setupEventListeners() {
    // Save button
    document.getElementById('saveBtn').addEventListener('click', saveConfig);
    
    // Add model button
    document.getElementById('addModelBtn').addEventListener('click', () => openModelModal(-1));
    
    // Reload button
    document.getElementById('reloadBtn').addEventListener('click', loadConfig);
    
    // Modal buttons
    document.getElementById('saveModelBtn').addEventListener('click', saveModel);
    document.getElementById('deleteModelBtn').addEventListener('click', deleteModel);
    document.getElementById('cancelModelBtn').addEventListener('click', closeModelModal);
    document.getElementById('addProviderBtn').addEventListener('click', addProviderField);
    
    // Modal close handlers
    document.querySelector('.close').addEventListener('click', closeModelModal);
    document.getElementById('modelModal').addEventListener('click', function(e) {
        if (e.target === this) closeModelModal();
    });
}

function setupTabs() {
    const tabButtons = document.querySelectorAll('.tab-btn');
    const tabContents = document.querySelectorAll('.tab-content');
    
    tabButtons.forEach(button => {
        button.addEventListener('click', () => {
            const tabId = button.getAttribute('data-tab');
            
            // Update active tab button
            tabButtons.forEach(btn => btn.classList.remove('active'));
            button.classList.add('active');
            
            // Show active tab content
            tabContents.forEach(content => {
                content.classList.remove('active');
                if (content.id === `${tabId}-tab`) {
                    content.classList.add('active');
                }
            });
            
            // Initialize sortable for the active tab if needed
            if (tabId === 'providers') {
                initProviderSortable();
            } else if (tabId === 'defaults') {
                initDefaultModelsSortable();
            }
        });
    });
}

async function loadConfig() {
    try {
        const response = await fetch('/api/models');
        if (!response.ok) throw new Error('Failed to load configuration');
        
        currentConfig = await response.json();
        renderConfig();
        showStatus('Configuration loaded successfully', 'success');
    } catch (error) {
        showStatus(`Error loading configuration: ${error.message}`, 'error');
        console.error('Error loading config:', error);
    }
}

function renderConfig() {
    if (!currentConfig) return;
    
    renderModels();
    renderProviders();
    renderDefaults();
}

function renderModels() {
    const container = document.getElementById('models-list');
    if (!currentConfig.models || currentConfig.models.length === 0) {
        container.innerHTML = '<div class="empty-state"><p>No models configured</p></div>';
        return;
    }
    
    container.innerHTML = currentConfig.models.map((model, index) => `
        <div class="model-card" onclick="openModelModal(${index})">
            <div class="model-header">
                <div class="model-name">${escapeHtml(model.name)}</div>
                <div class="model-command">/${escapeHtml(model.command)}</div>
            </div>
            <div class="model-features">
                ${model.vision ? '<span class="feature-tag vision">Vision</span>' : ''}
                ${model.reasoning ? '<span class="feature-tag reasoning">Reasoning</span>' : ''}
                ${model.is_llama ? '<span class="feature-tag llama">Llama</span>' : ''}
                ${model.is_markov ? '<span class="feature-tag">Markov</span>' : ''}
                ${model.is_eliza ? '<span class="feature-tag">Eliza</span>' : ''}
                ${model.limited ? '<span class="feature-tag">Limited</span>' : ''}
            </div>
            <div style="margin-top: 8px; font-size: 12px; color: #7f8c8d;">
                Providers: ${Object.keys(model.providers || {}).join(', ')}
            </div>
        </div>
    `).join('');
}

function renderProviders() {
    const container = document.getElementById('providers-list');
    if (!currentConfig.providers_order || currentConfig.providers_order.length === 0) {
        container.innerHTML = '<div class="empty-state"><p>No providers configured</p></div>';
        return;
    }
    
    container.innerHTML = currentConfig.providers_order.map(provider => `
        <li class="sortable-item">
            <span>${escapeHtml(provider)}</span>
        </li>
    `).join('');
    
    initProviderSortable();
}

function renderDefaults() {
    renderDefaultModels('default-models', currentConfig.default_models);
    renderDefaultModels('narrator-models', currentConfig.narrator_models);
    renderDefaultModels('vision-models', currentConfig.default_vision_models);
}

function renderDefaultModels(containerId, models) {
    const container = document.getElementById(containerId);
    if (!models || models.length === 0) {
        container.innerHTML = '<div class="empty-state"><p>No models selected</p></div>';
        return;
    }
    
    container.innerHTML = models.map(modelName => {
        const model = currentConfig.models.find(m => m.name === modelName);
        return `
            <div class="sortable-item">
                <span>${escapeHtml(modelName)}</span>
                ${model ? `<span style="font-size: 12px; color: #7f8c8d;">/${escapeHtml(model.command)}</span>` : ''}
            </div>
        `;
    }).join('');
    
    if (containerId === 'default-models') {
        initDefaultModelsSortable();
    }
}

function initProviderSortable() {
    const element = document.getElementById('providers-list');
    if (!element) return;
    
    Sortable.create(element, {
        animation: 150,
        ghostClass: 'sortable-ghost',
        chosenClass: 'sortable-chosen',
        onEnd: function(evt) {
            const newOrder = Array.from(element.children).map(li => 
                li.querySelector('span').textContent
            );
            currentConfig.providers_order = newOrder;
        }
    });
}

function initDefaultModelsSortable() {
    const containers = ['default-models', 'narrator-models', 'vision-models'];
    
    containers.forEach(containerId => {
        const element = document.getElementById(containerId);
        if (!element) return;
        
        Sortable.create(element, {
            animation: 150,
            ghostClass: 'sortable-ghost',
            chosenClass: 'sortable-chosen',
            group: containerId,
            onEnd: function(evt) {
                const newOrder = Array.from(element.children).map(div => 
                    div.querySelector('span').textContent
                );
                
                if (containerId === 'default-models') {
                    currentConfig.default_models = newOrder;
                } else if (containerId === 'narrator-models') {
                    currentConfig.narrator_models = newOrder;
                } else if (containerId === 'vision-models') {
                    currentConfig.default_vision_models = newOrder;
                }
            }
        });
    });
}

function openModelModal(index) {
    editingModelIndex = index;
    const modal = document.getElementById('modelModal');
    const title = document.getElementById('modalTitle');
    const deleteBtn = document.getElementById('deleteModelBtn');
    
    if (index === -1) {
        // New model
        title.textContent = 'Add New Model';
        deleteBtn.style.display = 'none';
        resetModelForm();
    } else {
        // Edit existing model
        title.textContent = 'Edit Model';
        deleteBtn.style.display = 'block';
        populateModelForm(currentConfig.models[index]);
    }
    
    modal.style.display = 'block';
}

function closeModelModal() {
    document.getElementById('modelModal').style.display = 'none';
}

function resetModelForm() {
    document.getElementById('modelForm').reset();
    document.getElementById('providers-container').innerHTML = '';
}

function populateModelForm(model) {
    document.getElementById('modelName').value = model.name || '';
    document.getElementById('modelCommand').value = model.command || '';
    document.getElementById('modelVision').checked = model.vision || false;
    document.getElementById('modelReasoning').checked = model.reasoning || false;
    document.getElementById('modelIsLlama').checked = model.is_llama || false;
    document.getElementById('modelIsMarkov').checked = model.is_markov || false;
    document.getElementById('modelIsEliza').checked = model.is_eliza || false;
    document.getElementById('modelLimited').checked = model.limited || false;
    document.getElementById('modelEncoding').value = model.encoding || '';
    
    // Populate providers
    const container = document.getElementById('providers-container');
    container.innerHTML = '';
    
    if (model.providers) {
        Object.entries(model.providers).forEach(([providerName, provider]) => {
            addProviderField(providerName, provider.codenames?.join(', ') || '');
        });
    }
}

function addProviderField(providerName = '', codenames = '') {
    const container = document.getElementById('providers-container');
    const providerId = Date.now();
    
    const html = `
        <div class="provider-item" data-id="${providerId}">
            <div class="provider-header">
                <input type="text" class="provider-name" placeholder="Provider name (e.g., openrouter)" value="${escapeHtml(providerName)}">
                <button type="button" class="remove-provider" onclick="removeProviderField(${providerId})">Remove</button>
            </div>
            <input type="text" class="codenames-input" placeholder="Codenames (comma-separated)" value="${escapeHtml(codenames)}">
        </div>
    `;
    
    container.insertAdjacentHTML('beforeend', html);
}

function removeProviderField(id) {
    const element = document.querySelector(`[data-id="${id}"]`);
    if (element) {
        element.remove();
    }
}

function saveModel() {
    const form = document.getElementById('modelForm');
    const formData = new FormData(form);
    
    const model = {
        name: formData.get('name'),
        command: formData.get('command'),
        vision: formData.get('vision') === 'on',
        reasoning: formData.get('reasoning') === 'on',
        is_llama: formData.get('is_llama') === 'on',
        is_markov: formData.get('is_markov') === 'on',
        is_eliza: formData.get('is_eliza') === 'on',
        limited: formData.get('limited') === 'on',
        encoding: formData.get('encoding') || '',
        providers: {}
    };
    
    // Get providers from form
    const providerElements = document.querySelectorAll('.provider-item');
    providerElements.forEach(element => {
        const providerName = element.querySelector('.provider-name').value.trim();
        const codenames = element.querySelector('.codenames-input').value.trim();
        
        if (providerName) {
            model.providers[providerName] = {
                codenames: codenames ? codenames.split(',').map(s => s.trim()).filter(s => s) : []
            };
        }
    });
    
    // Validate
    if (!model.name || !model.command) {
        showStatus('Model name and command are required', 'error');
        return;
    }
    
    // Update or add model
    if (editingModelIndex === -1) {
        // New model
        currentConfig.models.push(model);
    } else {
        // Update existing model
        currentConfig.models[editingModelIndex] = model;
    }
    
    closeModelModal();
    renderConfig();
    showStatus('Model saved successfully', 'success');
}

function deleteModel() {
    if (editingModelIndex === -1) return;
    
    if (confirm('Are you sure you want to delete this model?')) {
        currentConfig.models.splice(editingModelIndex, 1);
        closeModelModal();
        renderConfig();
        showStatus('Model deleted successfully', 'success');
    }
}

async function saveConfig() {
    try {
        const response = await fetch('/api/models/save', {
            method: 'POST',
            headers: {
                'Content-Type': 'application/json',
            },
            body: JSON.stringify(currentConfig)
        });
        
        if (!response.ok) {
            const error = await response.text();
            throw new Error(error);
        }
        
        showStatus('Configuration saved successfully', 'success');
    } catch (error) {
        showStatus(`Error saving configuration: ${error.message}`, 'error');
        console.error('Error saving config:', error);
    }
}

function showStatus(message, type) {
    // Remove existing status messages
    const existingMessages = document.querySelectorAll('.status-message');
    existingMessages.forEach(msg => msg.remove());
    
    const status = document.createElement('div');
    status.className = `status-message status-${type}`;
    status.textContent = message;
    
    document.querySelector('.container').insertBefore(status, document.querySelector('.tabs'));
    
    // Auto-remove after 5 seconds
    setTimeout(() => {
        if (status.parentNode) {
            status.remove();
        }
    }, 5000);
}

function escapeHtml(unsafe) {
    if (unsafe === null || unsafe === undefined) return '';
    return unsafe.toString()
        .replace(/&/g, "&")
        .replace(/</g, "<")
        .replace(/>/g, ">")
        .replace(/"/g, '"')
        .replace(/'/g, "&#039;");
}
