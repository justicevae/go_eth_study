// SPDX-License-Identifier: MIT
pragma solidity ^0.8.0;

import "@openzeppelin/contracts/access/AccessControl.sol";
import "@openzeppelin/contracts/utils/ReentrancyGuard.sol";
import "@openzeppelin/contracts/utils/Pausable.sol";
import "@openzeppelin/contracts/token/ERC20/IERC20.sol";
import "@openzeppelin/contracts/token/ERC20/utils/SafeERC20.sol";

contract StakeSystem is AccessControl, ReentrancyGuard, Pausable {
    using SafeERC20 for IERC20;

    // 常量
    bytes32 public constant ADMIN_ROLE = keccak256("ADMIN_ROLE");
    bytes32 public constant UPGRADER_ROLE = keccak256("UPGRADER_ROLE");
    
    // 代币地址
    IERC20 public metaNodeToken;
    
    // 质押池
    struct Pool {
        IERC20 stTokenAddress;      
        uint256 poolWeight;         
        uint256 lastRewardBlock;    
        uint256 accMetaNodePerST;   
        uint256 stTokenAmount;      
        uint256 minDepositAmount;   
        uint256 unstakeLockedBlocks;
        bool isActive;              
    }
    
    // 用户
    struct User {
        uint256 stAmount;        
        uint256 finishedMetaNode;
        uint256 pendingMetaNode; 
        uint256 rewardDebt;      
    }
    
    // 解质押
    struct UnstakeRequest {
        uint256 amount;     
        uint256 unlockBlock;
        bool claimed;       
    }
    
    Pool[] public pools;                                // 质押池数组
    mapping(uint256 => mapping(address => User)) public users; // 用户质押信息
    mapping(uint256 => mapping(address => UnstakeRequest[])) public unstakeRequests; // 解质押请求
    mapping(address => uint256) public userTotalMetaNode; // 用户总MetaNode奖励
    
    event PoolAdded(uint256 indexed pid, address indexed stToken);
    event PoolUpdated(uint256 indexed pid, uint256 newWeight, uint256 newMinDeposit, uint256 newLockedBlocks);
    event Deposited(address indexed user, uint256 indexed pid, uint256 amount);
    event UnstakeRequested(address indexed user, uint256 indexed pid, uint256 amount, uint256 unlockBlock);
    event UnstakeClaimed(address indexed user, uint256 indexed pid, uint256 amount);
    event RewardsClaimed(address indexed user, uint256 indexed pid, uint256 amount);
    event EmergencyWithdraw(address indexed user, uint256 indexed pid, uint256 amount);
    
    bool public depositPaused;
    bool public unstakePaused;
    bool public claimRewardsPaused;
    
    constructor(address _metaNodeToken, address _admin) {
        require(_metaNodeToken != address(0), "Invalid MetaNode token address");
        require(_admin != address(0), "Invalid admin address");
        
        metaNodeToken = IERC20(_metaNodeToken);
        
        _setupRole(DEFAULT_ADMIN_ROLE, _admin);
        _setupRole(ADMIN_ROLE, _admin);
        _setupRole(UPGRADER_ROLE, _admin);
    }
    
    modifier onlyAdmin() {
        require(hasRole(ADMIN_ROLE, msg.sender), "Caller is not an admin");
        _;
    }
    
    modifier onlyUpgrader() {
        require(hasRole(UPGRADER_ROLE, msg.sender), "Caller is not an upgrader");
        _;
    }
    
    modifier poolExists(uint256 _pid) {
        require(_pid < pools.length, "Pool does not exist");
        _;
    }
    
    modifier whenDepositNotPaused() {
        require(!depositPaused, "Deposit is paused");
        _;
    }
    
    modifier whenUnstakeNotPaused() {
        require(!unstakePaused, "Unstake is paused");
        _;
    }
    
    modifier whenClaimRewardsNotPaused() {
        require(!claimRewardsPaused, "Claim rewards is paused");
        _;
    }
    
    function poolLength() external view returns (uint256) {
        return pools.length;
    }
    
    function getUnstakeRequestCount(uint256 _pid, address _user) external view returns (uint256) {
        return unstakeRequests[_pid][_user].length;
    }
    
    function addPool(
        address _stTokenAddress,
        uint256 _poolWeight,
        uint256 _minDepositAmount,
        uint256 _unstakeLockedBlocks
    ) external onlyAdmin {
        require(_stTokenAddress != address(0), "Invalid stToken address");
        require(_poolWeight > 0, "Pool weight must be greater than 0");
        
        pools.push(Pool({
            stTokenAddress: IERC20(_stTokenAddress),
            poolWeight: _poolWeight,
            lastRewardBlock: block.number,
            accMetaNodePerST: 0,
            stTokenAmount: 0,
            minDepositAmount: _minDepositAmount,
            unstakeLockedBlocks: _unstakeLockedBlocks,
            isActive: true
        }));
        
        emit PoolAdded(pools.length - 1, _stTokenAddress);
    }
    
    function updatePool(
        uint256 _pid,
        uint256 _poolWeight,
        uint256 _minDepositAmount,
        uint256 _unstakeLockedBlocks
    ) external onlyAdmin poolExists(_pid) {
        require(_poolWeight > 0, "Pool weight must be greater than 0");
        
        Pool storage pool = pools[_pid];
        pool.poolWeight = _poolWeight;
        pool.minDepositAmount = _minDepositAmount;
        pool.unstakeLockedBlocks = _unstakeLockedBlocks;
        
        emit PoolUpdated(_pid, _poolWeight, _minDepositAmount, _unstakeLockedBlocks);
    }
    
    function setPoolActive(uint256 _pid, bool _isActive) external onlyAdmin poolExists(_pid) {
        pools[_pid].isActive = _isActive;
    }
    
    function deposit(uint256 _pid, uint256 _amount) 
        external 
        nonReentrant 
        whenNotPaused 
        whenDepositNotPaused 
        poolExists(_pid) 
    {
        require(_amount > 0, "Amount must be greater than 0");
        require(pools[_pid].isActive, "Pool is not active");
        
        Pool storage pool = pools[_pid];
        User storage user = users[_pid][msg.sender];
        
        require(_amount >= pool.minDepositAmount, "Amount below minimum deposit");
        updatePool(_pid);
        
        if (user.stAmount > 0) {
            uint256 pending = user.stAmount * pool.accMetaNodePerST / 1e12 - user.rewardDebt;
            if (pending > 0) {
                user.pendingMetaNode += pending;
            }
        }
        pool.stTokenAddress.safeTransferFrom(msg.sender, address(this), _amount);
        user.stAmount += _amount;
        pool.stTokenAmount += _amount;
        user.rewardDebt = user.stAmount * pool.accMetaNodePerST / 1e12;
        
        emit Deposited(msg.sender, _pid, _amount);
    }
    
    function requestUnstake(uint256 _pid, uint256 _amount) 
        external 
        nonReentrant 
        whenNotPaused 
        whenUnstakeNotPaused 
        poolExists(_pid) 
    {
        require(_amount > 0, "Amount must be greater than 0");
        
        Pool storage pool = pools[_pid];
        User storage user = users[_pid][msg.sender];
        
        require(user.stAmount >= _amount, "Insufficient staked amount");
        updatePool(_pid);
        
        uint256 pending = user.stAmount * pool.accMetaNodePerST / 1e12 - user.rewardDebt;
        if (pending > 0) {
            user.pendingMetaNode += pending;
        }
        user.stAmount -= _amount;
        pool.stTokenAmount -= _amount;
        
        uint256 unlockBlock = block.number + pool.unstakeLockedBlocks;
        unstakeRequests[_pid][msg.sender].push(UnstakeRequest({
            amount: _amount,
            unlockBlock: unlockBlock,
            claimed: false
        }));
        user.rewardDebt = user.stAmount * pool.accMetaNodePerST / 1e12;
        emit UnstakeRequested(msg.sender, _pid, _amount, unlockBlock);
    }
    
    function claimUnstake(uint256 _pid, uint256 _requestIndex) 
        external 
        nonReentrant 
        whenNotPaused 
        poolExists(_pid) 
    {
        UnstakeRequest storage request = unstakeRequests[_pid][msg.sender][_requestIndex];
        
        require(!request.claimed, "Already claimed");
        require(block.number >= request.unlockBlock, "Still locked");
        
        uint256 amount = request.amount;
        request.claimed = true;
        pools[_pid].stTokenAddress.safeTransfer(msg.sender, amount);
        
        emit UnstakeClaimed(msg.sender, _pid, amount);
    }
    
    function claimRewards(uint256 _pid) 
        external 
        nonReentrant 
        whenNotPaused 
        whenClaimRewardsNotPaused 
        poolExists(_pid) 
    {
        updatePool(_pid);
        
        User storage user = users[_pid][msg.sender];
        uint256 pending = user.stAmount * pools[_pid].accMetaNodePerST / 1e12 - user.rewardDebt;
        if (pending > 0) {
            user.pendingMetaNode += pending;
        }
        user.rewardDebt = user.stAmount * pools[_pid].accMetaNodePerST / 1e12;
        uint256 rewardAmount = user.pendingMetaNode;
        if (rewardAmount > 0) {
            user.pendingMetaNode = 0;
            user.finishedMetaNode += rewardAmount;
            userTotalMetaNode[msg.sender] += rewardAmount;
            
            metaNodeToken.safeTransfer(msg.sender, rewardAmount);
            
            emit RewardsClaimed(msg.sender, _pid, rewardAmount);
        }
    }
    
    function emergencyWithdraw(uint256 _pid) external nonReentrant poolExists(_pid) {
        User storage user = users[_pid][msg.sender];
        Pool storage pool = pools[_pid];
        
        uint256 amount = user.stAmount;
        require(amount > 0, "No staked amount");
        
        user.stAmount = 0;
        user.rewardDebt = 0;
        user.pendingMetaNode = 0;
        pool.stTokenAmount -= amount;
        pool.stTokenAddress.safeTransfer(msg.sender, amount);
        
        emit EmergencyWithdraw(msg.sender, _pid, amount);
    }
    
    function updatePool(uint256 _pid) public {
        Pool storage pool = pools[_pid];
        if (block.number <= pool.lastRewardBlock) {
            return;
        }
        
        if (pool.stTokenAmount == 0) {
            pool.lastRewardBlock = block.number;
            return;
        }
        uint256 metaNodeReward = calculateReward(_pid);
        pool.accMetaNodePerST += metaNodeReward * 1e12 / pool.stTokenAmount;
        pool.lastRewardBlock = block.number;
    }
    
    function calculateReward(uint256 _pid) internal view returns (uint256) {
        Pool memory pool = pools[_pid];
        uint256 blockDelta = block.number - pool.lastRewardBlock;
        uint256 totalRewardPerBlock = 100 * 1e18;
        uint256 totalWeight = getTotalWeight();
        if (totalWeight == 0) return 0;
        
        uint256 poolReward = totalRewardPerBlock * blockDelta * pool.poolWeight / totalWeight;
        return poolReward;
    }
    
    function getTotalWeight() public view returns (uint256) {
        uint256 totalWeight = 0;
        for (uint256 i = 0; i < pools.length; i++) {
            if (pools[i].isActive) {
                totalWeight += pools[i].poolWeight;
            }
        }
        return totalWeight;
    }
    
    function pendingRewards(uint256 _pid, address _user) external view returns (uint256) {
        Pool storage pool = pools[_pid];
        User storage user = users[_pid][_user];
        
        uint256 accMetaNodePerST = pool.accMetaNodePerST;
        if (block.number > pool.lastRewardBlock && pool.stTokenAmount != 0) {
            uint256 metaNodeReward = calculateReward(_pid);
            accMetaNodePerST += metaNodeReward * 1e12 / pool.stTokenAmount;
        }
        
        return user.pendingMetaNode + (user.stAmount * accMetaNodePerST / 1e12 - user.rewardDebt);
    }
    
    function setDepositPaused(bool _paused) external onlyAdmin {
        depositPaused = _paused;
    }
    
    function setUnstakePaused(bool _paused) external onlyAdmin {
        unstakePaused = _paused;
    }
    
    function setClaimRewardsPaused(bool _paused) external onlyAdmin {
        claimRewardsPaused = _paused;
    }
    
    function rescueTokens(address _token, uint256 _amount) external onlyAdmin {
        require(_token != address(metaNodeToken), "Cannot rescue MetaNode tokens");
        
        for (uint256 i = 0; i < pools.length; i++) {
            require(_token != address(pools[i].stTokenAddress), "Cannot rescue staking tokens");
        }
        
        IERC20(_token).safeTransfer(msg.sender, _amount);
    }
    
    function upgrade() external onlyUpgrader {
    }
}