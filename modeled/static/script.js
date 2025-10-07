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
    
    // Version edit button
    document.getElementById('editVersionBtn').addEventListener('click', openVersionEditModal);
    
    // Modal buttons
    document.getElementById('saveModelBtn').addEventListener('click', saveModel);
    document.getElementById('deleteModelBtn').addEventListener('click', deleteModel);
    document.getElementById('cancelModelBtn').addEventListener('click', closeModelModal);
    document.getElementById('addProviderBtn').addEventListener('click', () => addProviderField());
    
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
            if (tabId === 'models') {
                initModelsSortable();
            } else if (tabId === 'providers') {
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
    
    renderVersion();
    renderModels();
    renderProviders();
    renderDefaults();
}

function renderVersion() {
    const display = document.getElementById('currentVersionDisplay');
    if (display && currentConfig.current_version !== undefined) {
        display.textContent = currentConfig.current_version;
    }
}

function renderModels() {
    const container = document.getElementById('models-list');
    if (!currentConfig.models || currentConfig.models.length === 0) {
        container.innerHTML = '<div class="empty-state"><p>No models configured</p></div>';
        return;
    }
    
    container.innerHTML = currentConfig.models.map((model, index) => `
        <div class="model-card sortable-item" data-index="${index}" onclick="openModelModal(${index})">
            <div class="model-header">
                <div class="model-name">${escapeHtml(model.name)}</div>
                <div class="model-command">/${escapeHtml(model.command)}</div>
            </div>
            <div class="model-features">
                ${model.vision ? '<span class="feature-tag vision">Vision</span>' : ''}
                ${model.reasoning ? '<span class="feature-tag reasoning">Reasoning</span>' : ''}
                ${model.is_markov ? '<span class="feature-tag">Markov</span>' : ''}
                ${model.is_eliza ? '<span class="feature-tag">Eliza</span>' : ''}
                ${model.limited ? '<span class="feature-tag">Limited</span>' : ''}
            </div>
            <div style="margin-top: 8px; font-size: 12px; color: #7f8c8d;">
                Providers: ${Object.keys(model.providers || {}).join(', ')}
            </div>
        </div>
    `).join('');
    
    initModelsSortable();
}

function renderProviders() {
    const container = document.getElementById('providers-list');
    
    // Add header with add button
    const header = document.createElement('div');
    header.className = 'section-header';
    header.innerHTML = `
        <button class="btn btn-secondary btn-sm" onclick="openAddProviderModal()">Add Provider</button>
    `;
    
    // Clear and rebuild container
    container.innerHTML = '';
    container.appendChild(header);
    
    if (!currentConfig.providers_order || currentConfig.providers_order.length === 0) {
        const emptyState = document.createElement('div');
        emptyState.className = 'empty-state';
        emptyState.innerHTML = '<p>No providers configured</p>';
        container.appendChild(emptyState);
        return;
    }
    
    const listContainer = document.createElement('ul');
    listContainer.className = 'sortable-list';
    
    listContainer.innerHTML = currentConfig.providers_order.map(provider => `
        <li class="sortable-item">
            <span>${escapeHtml(provider)}</span>
            <button class="remove-provider" onclick="removeProvider('${escapeHtml(provider)}')" title="Remove provider">×</button>
        </li>
    `).join('');
    
    container.appendChild(listContainer);
    initProviderSortable();
}

function renderDefaults() {
    renderDefaultModels('default-models', currentConfig.default_models, 'default_models');
    renderDefaultModels('narrator-models', currentConfig.narrator_models, 'narrator_models');
    renderDefaultModels('vision-models', currentConfig.default_vision_models, 'default_vision_models');
}

function renderDefaultModels(containerId, models, configKey) {
    const container = document.getElementById(containerId);
    
    // Add header with add button
    const header = document.createElement('div');
    header.className = 'section-header';
    header.innerHTML = `
        <button class="btn btn-secondary btn-sm" onclick="openAddModelModal('${configKey}')">Add Model</button>
    `;
    
    // Clear and rebuild container
    container.innerHTML = '';
    container.appendChild(header);
    
    if (!models || models.length === 0) {
        const emptyState = document.createElement('div');
        emptyState.className = 'empty-state';
        emptyState.innerHTML = '<p>No models selected</p>';
        container.appendChild(emptyState);
        return;
    }
    
    const listContainer = document.createElement('div');
    listContainer.className = 'sortable-list';
    
    listContainer.innerHTML = models.map(modelName => {
        const model = currentConfig.models.find(m => m.name === modelName);
        return `
            <div class="sortable-item">
                <span>${escapeHtml(modelName)}</span>
                ${model ? `<span style="font-size: 12px; color: #7f8c8d;">/${escapeHtml(model.command)}</span>` : ''}
                <button class="remove-default-model" onclick="removeFromDefaultModels('${configKey}', '${escapeHtml(modelName)}')" title="Remove from list">×</button>
            </div>
        `;
    }).join('');
    
    container.appendChild(listContainer);
    
    if (containerId === 'default-models') {
        initDefaultModelsSortable();
    }
}

function initProviderSortable() {
    const container = document.getElementById('providers-list');
    if (!container) return;
    
    const listContainer = container.querySelector('.sortable-list');
    if (!listContainer) return;
    
    Sortable.create(listContainer, {
        animation: 150,
        ghostClass: 'sortable-ghost',
        chosenClass: 'sortable-chosen',
        onEnd: function(evt) {
            const newOrder = Array.from(listContainer.children).map(li =>
                li.querySelector('span').textContent
            );
            currentConfig.providers_order = newOrder;
        }
    });
}

function openAddProviderModal() {
    const modal = document.createElement('div');
    modal.className = 'modal';
    modal.style.display = 'block';
    modal.innerHTML = `
        <div class="modal-content" style="max-width: 400px;">
            <div class="modal-header">
                <h3>Add Provider</h3>
                <span class="close" onclick="closeAddProviderModal()">&times;</span>
            </div>
            <div class="modal-body">
                <div class="form-group">
                    <label>Provider Name:</label>
                    <input type="text" id="providerName" style="width: 100%; padding: 8px;" placeholder="Enter provider name">
                </div>
            </div>
            <div class="modal-footer">
                <button type="button" class="btn btn-primary" onclick="addProvider()">Add Provider</button>
                <button type="button" class="btn btn-secondary" onclick="closeAddProviderModal()">Cancel</button>
            </div>
        </div>
    `;
    
    document.body.appendChild(modal);
    
    // Close modal when clicking outside
    modal.addEventListener('click', function(e) {
        if (e.target === this) {
            closeAddProviderModal();
        }
    });
}

function closeAddProviderModal() {
    const modal = document.querySelector('.modal');
    if (modal) {
        modal.remove();
    }
}

function addProvider() {
    const providerName = document.getElementById('providerName').value.trim();
    
    if (!providerName) {
        showStatus('Provider name is required', 'error');
        return;
    }
    
    if (!currentConfig.providers_order) {
        currentConfig.providers_order = [];
    }
    
    if (currentConfig.providers_order.includes(providerName)) {
        showStatus('Provider already exists', 'error');
        return;
    }
    
    currentConfig.providers_order.push(providerName);
    
    // Close modal and refresh
    closeAddProviderModal();
    renderConfig();
    showStatus('Provider added successfully', 'success');
}

function removeProvider(providerName) {
    if (!currentConfig.providers_order) return;
    
    const index = currentConfig.providers_order.indexOf(providerName);
    if (index > -1) {
        currentConfig.providers_order.splice(index, 1);
        renderConfig();
        showStatus('Provider removed successfully', 'success');
    }
}

function initModelsSortable() {
    const element = document.getElementById('models-list');
    if (!element) return;
    
    Sortable.create(element, {
        animation: 150,
        ghostClass: 'sortable-ghost',
        chosenClass: 'sortable-chosen',
        onEnd: function(evt) {
            const newOrder = Array.from(element.children).map((card, index) => {
                const modelIndex = parseInt(card.getAttribute('data-index'));
                return currentConfig.models[modelIndex];
            });
            currentConfig.models = newOrder;
            renderModels(); // Re-render to update data-index attributes
        }
    });
}

function initDefaultModelsSortable() {
    const containers = ['default-models', 'narrator-models', 'vision-models'];
    
    containers.forEach(containerId => {
        const element = document.getElementById(containerId);
        if (!element) return;
        
        const listContainer = element.querySelector('.sortable-list');
        if (!listContainer) return;
        
        Sortable.create(listContainer, {
            animation: 150,
            ghostClass: 'sortable-ghost',
        chosenClass: 'sortable-chosen',
        group: containerId,
        onEnd: function(evt) {
                const newOrder = Array.from(listContainer.children).map(div =>
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

function openAddModelModal(configKey) {
    const availableModels = currentConfig.models.filter(model => {
        // Filter based on config key
        if (configKey === 'default_vision_models' && !model.vision) {
            return false;
        }
        // Don't show models that are already in the list
        const currentList = getCurrentList(configKey);
        return !currentList.includes(model.name);
    });
    
    if (availableModels.length === 0) {
        showStatus('No available models to add', 'error');
        return;
    }
    
    const modal = document.createElement('div');
    modal.className = 'modal';
    modal.style.display = 'block';
    modal.innerHTML = `
        <div class="modal-content" style="max-width: 400px;">
            <div class="modal-header">
                <h3>Add Model to ${getListDisplayName(configKey)}</h3>
                <span class="close" onclick="this.parentElement.parentElement.parentElement.remove()">&times;</span>
            </div>
            <div class="modal-body">
                <div class="form-group">
                    <label>Select Model:</label>
                    <select id="modelSelect" style="width: 100%; padding: 8px;">
                        ${availableModels.map(model =>
                            `<option value="${escapeHtml(model.name)}">${escapeHtml(model.name)} (/${escapeHtml(model.command)})</option>`
                        ).join('')}
                    </select>
                </div>
            </div>
            <div class="modal-footer">
                <button type="button" class="btn btn-primary" onclick="addToDefaultModels('${configKey}')">Add Model</button>
                <button type="button" class="btn btn-secondary" onclick="this.parentElement.parentElement.parentElement.parentElement.remove()">Cancel</button>
            </div>
        </div>
    `;
    
    document.body.appendChild(modal);
    
    // Close modal when clicking outside
    modal.addEventListener('click', function(e) {
        if (e.target === this) {
            this.remove();
        }
    });
}

function getCurrentList(configKey) {
    switch(configKey) {
        case 'default_models': return currentConfig.default_models || [];
        case 'narrator_models': return currentConfig.narrator_models || [];
        case 'default_vision_models': return currentConfig.default_vision_models || [];
        default: return [];
    }
}

function getListDisplayName(configKey) {
    switch(configKey) {
        case 'default_models': return 'Default Models';
        case 'narrator_models': return 'Narrator Models';
        case 'default_vision_models': return 'Vision Models';
        default: return 'List';
    }
}

function addToDefaultModels(configKey) {
    const select = document.getElementById('modelSelect');
    const modelName = select.value;
    
    if (!modelName) return;
    
    const currentList = getCurrentList(configKey);
    if (currentList.includes(modelName)) {
        showStatus('Model is already in the list', 'error');
        return;
    }
    
    currentList.push(modelName);
    
    // Update the config
    switch(configKey) {
        case 'default_models':
            currentConfig.default_models = currentList;
            break;
        case 'narrator_models':
            currentConfig.narrator_models = currentList;
            break;
        case 'default_vision_models':
            currentConfig.default_vision_models = currentList;
            break;
    }
    
    // Close modal and refresh
    document.querySelector('.modal').remove();
    renderConfig();
    showStatus('Model added successfully', 'success');
}

function removeFromDefaultModels(configKey, modelName) {
    const currentList = getCurrentList(configKey);
    const index = currentList.indexOf(modelName);
    
    if (index > -1) {
        currentList.splice(index, 1);
        
        // Update the config
        switch(configKey) {
            case 'default_models':
                currentConfig.default_models = currentList;
                break;
            case 'narrator_models':
                currentConfig.narrator_models = currentList;
                break;
            case 'default_vision_models':
                currentConfig.default_vision_models = currentList;
                break;
        }
        
        renderConfig();
        showStatus('Model removed successfully', 'success');
    }
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
                <button type="button" class="remove-provider" onclick="removeProviderField(${providerId})">x</button>
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

function openVersionEditModal() {
    const modal = document.createElement('div');
    modal.className = 'modal';
    modal.style.display = 'block';
    modal.innerHTML = `
        <div class="modal-content" style="max-width: 400px;">
            <div class="modal-header">
                <h3>Edit Current Version</h3>
                <span class="close" onclick="this.closest('.modal').remove()">&times;</span>
            </div>
            <div class="modal-body">
                <div class="form-group">
                    <label>Current Version:</label>
                    <input type="number" id="versionInput" style="width: 100%; padding: 8px;"
                           value="${currentConfig.current_version || 6}" min="1" step="1">
                </div>
            </div>
            <div class="modal-footer">
                <button type="button" class="btn btn-primary" onclick="saveVersion()">Save Version</button>
                <button type="button" class="btn btn-secondary" onclick="this.closest('.modal').remove()">Cancel</button>
            </div>
        </div>
    `;
    
    document.body.appendChild(modal);
    
    // Close modal when clicking outside
    modal.addEventListener('click', function(e) {
        if (e.target === this) {
            this.remove();
        }
    });
}

function saveVersion() {
    const versionInput = document.getElementById('versionInput');
    const newVersion = parseInt(versionInput.value);
    
    if (isNaN(newVersion) || newVersion < 1) {
        showStatus('Please enter a valid version number (minimum 1)', 'error');
        return;
    }
    
    currentConfig.current_version = newVersion;
    
    // Close the modal
    const modal = document.querySelector('.modal');
    if (modal && modal.querySelector('#versionInput')) {
        modal.remove();
    }
    
    renderVersion();
    showStatus('Version updated successfully', 'success');
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
